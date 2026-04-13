package local

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/thand-io/agent/internal/models"
	"github.com/thand-io/agent/internal/providers"
)

const LocalProviderName = "local"

var localSudoPermission = models.ProviderPermission{
	ID:          "local-sudo",
	Name:        "local:sudo:*",
	Title:       "Local sudo access",
	Description: "Managed local sudo and privileged command execution",
}

type commandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type localProvider struct {
	*models.BaseProvider

	lookPath       func(string) (string, error)
	runCommand     func(name string, args ...string) (commandResult, error)
	lookupUser     func(string) (*osuser.User, error)
	readFile       func(string) ([]byte, error)
	goos           func() string
	removeFile     func(string) error
	renameFile     func(string, string) error
	writeFile      func(string, []byte, os.FileMode) error
	createTempFile func(dir, pattern string) (*os.File, error)
	chmodFile      func(string, os.FileMode) error
}

func (p *localProvider) Initialize(identifier string, provider models.ProviderConfig) error {
	p.BaseProvider = models.NewBaseProvider(identifier, provider, LocalCapabilities)
	p.lookPath = exec.LookPath
	p.runCommand = runSystemCommand
	p.lookupUser = osuser.Lookup
	p.readFile = os.ReadFile
	p.goos = func() string { return runtime.GOOS }
	p.removeFile = os.Remove
	p.renameFile = os.Rename
	p.writeFile = os.WriteFile
	p.createTempFile = os.CreateTemp
	p.chmodFile = os.Chmod
	p.SetPermissions([]models.ProviderPermission{localSudoPermission})
	return nil
}

func (p *localProvider) AuthorizeRole(
	ctx models.ProviderContext,
	req *models.AuthorizeRoleRequest,
) (*models.AuthorizeRoleResponse, error) {
	if !req.IsValid() {
		return nil, fmt.Errorf("user and role must be provided to authorize local sudo access")
	}

	meta, err := decodeLocalSudoRequestMetadata(req.Metadata)
	if err != nil {
		return nil, err
	}

	switch p.goos() {
	case "linux", "darwin":
		return p.authorizeUnix(req, meta)
	case "windows":
		return p.authorizeWindows(req, meta)
	default:
		return nil, fmt.Errorf("local sudo is not supported on %s", p.goos())
	}
}

func (p *localProvider) RevokeRole(
	ctx models.ProviderContext,
	req *models.RevokeRoleRequest,
) (*models.RevokeRoleResponse, error) {
	if req == nil || req.AuthorizeRoleResponse == nil || len(req.AuthorizeRoleResponse.Metadata) == 0 {
		return &models.RevokeRoleResponse{}, nil
	}

	meta, err := decodeLocalSudoAuthorizationMetadata(req.AuthorizeRoleResponse.Metadata)
	if err != nil {
		return nil, err
	}

	if len(meta.SudoersPath) == 0 {
		return &models.RevokeRoleResponse{}, nil
	}

	if err := p.removeFile(meta.SudoersPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to revoke local sudo access: %w", err)
	}

	return &models.RevokeRoleResponse{}, nil
}

func (p *localProvider) authorizeUnix(
	req *models.AuthorizeRoleRequest,
	meta models.LocalSudoRequestMetadata,
) (*models.AuthorizeRoleResponse, error) {
	username, err := p.targetUsername(req)
	if err != nil {
		return nil, err
	}

	switch meta.Mode {
	case models.LocalSudoModeTimed:
		sudoersPath, err := p.installSudoersGrant(username, nil, req.Role.GetName(), meta.GrantID)
		if err != nil {
			return nil, err
		}

		authMeta := models.LocalSudoAuthorizationMetadata{
			Platform:    p.goos(),
			Mode:        string(meta.Mode),
			GrantID:     meta.GrantID,
			Username:    username,
			SudoersPath: sudoersPath,
		}

		return localAuthorizeResponse(req, authMeta), nil
	case models.LocalSudoModeCommand:
		if len(meta.Command) == 0 {
			return nil, fmt.Errorf("privileged command mode requires a command")
		}

		resolvedCommand, err := p.resolveCommand(meta.Command)
		if err != nil {
			return nil, err
		}

		sudoersPath, err := p.installSudoersGrant(username, resolvedCommand, req.Role.GetName(), meta.GrantID)
		if err != nil {
			return nil, err
		}

		defer func() {
			if err := p.removeFile(sudoersPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				logrus.WithError(err).Warn("failed to clean up sudoers grant after command execution")
			}
		}()

		result, runErr := p.runCommand(p.sudoExecutable(), resolvedCommand...)

		authMeta := models.LocalSudoAuthorizationMetadata{
			Platform:    p.goos(),
			Mode:        string(meta.Mode),
			GrantID:     meta.GrantID,
			Username:    username,
			SudoersPath: sudoersPath,
			Command:     resolvedCommand,
			Stdout:      result.Stdout,
			Stderr:      result.Stderr,
			ExitCode:    result.ExitCode,
			Immediate:   true,
		}

		if runErr != nil {
			return nil, fmt.Errorf("privileged command failed: %w\nstdout:\n%s\nstderr:\n%s", runErr, result.Stdout, result.Stderr)
		}

		authMeta.SudoersPath = ""
		return localAuthorizeResponse(req, authMeta), nil
	default:
		return nil, fmt.Errorf("unsupported local sudo mode %q", meta.Mode)
	}
}

func (p *localProvider) authorizeWindows(
	req *models.AuthorizeRoleRequest,
	meta models.LocalSudoRequestMetadata,
) (*models.AuthorizeRoleResponse, error) {
	if meta.Mode == models.LocalSudoModeTimed {
		return nil, fmt.Errorf("timed sudo access is not supported on Windows in v1; use a brokered command instead")
	}

	if len(meta.Command) == 0 {
		return nil, fmt.Errorf("Windows sudo requires a command")
	}

	sudoPath, err := p.resolveExecutable(p.GetConfig().GetStringWithDefault("sudo_path", "sudo"), []string{"sudo"})
	if err != nil {
		return nil, fmt.Errorf("Windows Sudo is unavailable: %w", err)
	}

	resolvedCommand, err := p.resolveCommand(meta.Command)
	if err != nil {
		return nil, err
	}

	result, runErr := p.runCommand(sudoPath, resolvedCommand...)
	authMeta := models.LocalSudoAuthorizationMetadata{
		Platform:  p.goos(),
		Mode:      string(meta.Mode),
		GrantID:   meta.GrantID,
		Command:   resolvedCommand,
		Stdout:    result.Stdout,
		Stderr:    result.Stderr,
		ExitCode:  result.ExitCode,
		Immediate: true,
	}

	if runErr != nil {
		return nil, fmt.Errorf("Windows sudo command failed: %w\nstdout:\n%s\nstderr:\n%s", runErr, result.Stdout, result.Stderr)
	}

	return localAuthorizeResponse(req, authMeta), nil
}

func localAuthorizeResponse(
	req *models.AuthorizeRoleRequest,
	meta models.LocalSudoAuthorizationMetadata,
) *models.AuthorizeRoleResponse {
	response := &models.AuthorizeRoleResponse{
		Roles:    []string{req.Role.GetName()},
		Metadata: map[string]any{},
	}

	if req.Identity != nil && req.Identity.User != nil {
		response.UserId = req.Identity.User.GetMappableIdentifier()
	}

	if len(meta.Platform) > 0 {
		response.Metadata["platform"] = meta.Platform
	}
	if len(meta.Mode) > 0 {
		response.Metadata["mode"] = meta.Mode
	}
	if len(meta.GrantID) > 0 {
		response.Metadata["grant_id"] = meta.GrantID
	}
	if len(meta.Username) > 0 {
		response.Metadata["username"] = meta.Username
	}
	if len(meta.SudoersPath) > 0 {
		response.Metadata["sudoers_path"] = meta.SudoersPath
	}
	if len(meta.Command) > 0 {
		response.Metadata["command"] = append([]string(nil), meta.Command...)
	}
	if len(meta.Stdout) > 0 {
		response.Metadata["stdout"] = meta.Stdout
	}
	if len(meta.Stderr) > 0 {
		response.Metadata["stderr"] = meta.Stderr
	}
	if meta.ExitCode != 0 {
		response.Metadata["exit_code"] = meta.ExitCode
	}
	if meta.Immediate {
		response.Metadata["immediate"] = true
	}

	return response
}

type uidRange struct {
	Min int
	Max int
}

func (p *localProvider) targetUsername(req *models.AuthorizeRoleRequest) (string, error) {
	if configured := p.GetConfig().GetStringWithDefault("username", ""); len(configured) > 0 {
		return p.validateTargetUsername(configured, "configured local username")
	}

	identityUsername := usernameFromIdentity(req)
	if len(identityUsername) == 0 {
		return "", fmt.Errorf("local username is not available from config or trusted identity")
	}

	trustedProviderID, trusted := p.trustedUsernameProviderID(req)
	if !trusted {
		providerIDs := identityProviderIDs(req)
		if len(providerIDs) == 0 {
			return "", fmt.Errorf("identity username is unavailable for local sudo because the identity has no trusted provider ids")
		}
		return "", fmt.Errorf("identity username provider ids %v are not trusted for local sudo", providerIDs)
	}

	return p.validateTargetUsername(identityUsername, fmt.Sprintf("identity username from provider %q", trustedProviderID))
}

func usernameFromIdentity(req *models.AuthorizeRoleRequest) string {
	if req == nil || req.Identity == nil || req.Identity.User == nil {
		return ""
	}
	return strings.TrimSpace(req.Identity.User.Username)
}

func (p *localProvider) validateTargetUsername(username string, source string) (string, error) {
	username = strings.TrimSpace(username)
	if len(username) == 0 || strings.ContainsAny(username, " \t\r\n") {
		return "", fmt.Errorf("%s %q is invalid", source, username)
	}

	if p.isDeniedUsername(username) {
		return "", fmt.Errorf("local username %q is denied for local sudo", username)
	}

	foundUser, err := p.lookupUser(username)
	if err != nil {
		return "", fmt.Errorf("%s %q could not be resolved: %w", source, username, err)
	}
	if foundUser == nil {
		return "", fmt.Errorf("%s %q could not be resolved", source, username)
	}

	if err := p.validateAllowedUID(foundUser); err != nil {
		return "", err
	}

	if resolved := strings.TrimSpace(foundUser.Username); resolved != "" {
		return resolved, nil
	}

	return username, nil
}

func (p *localProvider) trustedUsernameProviderID(req *models.AuthorizeRoleRequest) (string, bool) {
	configured, found := p.GetConfig().GetStringSlice("trusted_username_sources")
	if !found || len(configured) == 0 {
		return "", false
	}

	providerIDs := identityProviderIDs(req)
	for _, providerID := range providerIDs {
		for _, allowed := range configured {
			if strings.EqualFold(strings.TrimSpace(allowed), providerID) {
				return providerID, true
			}
		}
	}
	return "", false
}

func identityProviderIDs(req *models.AuthorizeRoleRequest) []string {
	if req == nil || req.Identity == nil || len(req.Identity.Providers) == 0 {
		return nil
	}

	ids := make([]string, 0, len(req.Identity.Providers))
	for providerID := range req.Identity.Providers {
		if trimmed := strings.TrimSpace(providerID); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	sort.Strings(ids)
	return ids
}

func (p *localProvider) isDeniedUsername(username string) bool {
	denied := []string{"root", "daemon", "nobody"}
	if configured, found := p.GetConfig().GetStringSlice("denied_usernames"); found && len(configured) > 0 {
		denied = append(denied, configured...)
	}

	for _, candidate := range denied {
		if strings.EqualFold(strings.TrimSpace(candidate), username) {
			return true
		}
	}
	return false
}

func (p *localProvider) validateAllowedUID(foundUser *osuser.User) error {
	if foundUser == nil {
		return fmt.Errorf("local user lookup returned nil user")
	}
	uid, err := strconv.Atoi(foundUser.Uid)
	if err != nil {
		return fmt.Errorf("failed to parse UID %q for local user %q: %w", foundUser.Uid, foundUser.Username, err)
	}

	ranges, err := p.allowedUIDRanges()
	if err != nil {
		return err
	}

	for _, allowed := range ranges {
		if uid >= allowed.Min && uid <= allowed.Max {
			return nil
		}
	}

	return fmt.Errorf("local user %q with UID %d is outside allowed UID ranges", foundUser.Username, uid)
}

func (p *localProvider) allowedUIDRanges() ([]uidRange, error) {
	if configured, found := p.GetConfig().GetStringSlice("allowed_uid_ranges"); found && len(configured) > 0 {
		return parseUIDRanges(configured)
	}

	if p.goos() != "windows" {
		if derived, err := p.allowedUIDRangesFromLoginDefs(); err == nil && len(derived) > 0 {
			return derived, nil
		}
	}

	switch p.goos() {
	case "darwin":
		return []uidRange{{Min: 500, Max: 60000}}, nil
	default:
		return []uidRange{{Min: 1000, Max: 60000}}, nil
	}
}

func (p *localProvider) allowedUIDRangesFromLoginDefs() ([]uidRange, error) {
	data, err := p.readFile("/etc/login.defs")
	if err != nil {
		return nil, err
	}

	var uidMin, uidMax int
	var hasMin, hasMax bool

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "UID_MIN":
			value, err := strconv.Atoi(fields[1])
			if err == nil {
				uidMin = value
				hasMin = true
			}
		case "UID_MAX":
			value, err := strconv.Atoi(fields[1])
			if err == nil {
				uidMax = value
				hasMax = true
			}
		}
	}

	if !hasMin || !hasMax || uidMin > uidMax {
		return nil, fmt.Errorf("login.defs did not define a valid UID_MIN/UID_MAX range")
	}

	return []uidRange{{Min: uidMin, Max: uidMax}}, nil
}

func parseUIDRanges(values []string) ([]uidRange, error) {
	ranges := make([]uidRange, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if strings.Contains(value, "-") {
			parts := strings.SplitN(value, "-", 2)
			minValue, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid UID range %q", value)
			}
			maxValue, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid UID range %q", value)
			}
			if minValue > maxValue {
				return nil, fmt.Errorf("invalid UID range %q", value)
			}
			ranges = append(ranges, uidRange{Min: minValue, Max: maxValue})
			continue
		}

		exactValue, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid UID value %q", value)
		}
		ranges = append(ranges, uidRange{Min: exactValue, Max: exactValue})
	}

	if len(ranges) == 0 {
		return nil, fmt.Errorf("no valid UID ranges configured")
	}
	return ranges, nil
}

func (p *localProvider) installSudoersGrant(username string, command []string, roleName string, grantID string) (string, error) {
	sudoersDir := p.GetConfig().GetStringWithDefault("sudoers_dir", "/etc/sudoers.d")
	if len(sudoersDir) == 0 {
		return "", fmt.Errorf("sudoers directory is not configured")
	}

	// Stop-gap: overlapping grants for the same local user are allowed because the
	// current timed sudo policy materializes the same effective sudoers rule.
	// Revisit this if per-grant policy becomes variable.
	fileName, err := sudoersFragmentName(roleName, command, grantID)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(sudoersDir, fileName)
	content, err := p.buildSudoersContent(username, command)
	if err != nil {
		return "", err
	}

	tempFile, err := p.createTempFile(sudoersDir, ".thand-sudo-*")
	if err != nil {
		return "", fmt.Errorf("failed to create sudoers temp file: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()

	cleanupTemp := func() {
		if err := p.removeFile(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logrus.WithError(err).Warn("failed to clean up temporary sudoers file")
		}
	}

	if err := p.writeFile(tempPath, []byte(content), 0440); err != nil {
		cleanupTemp()
		return "", fmt.Errorf("failed to write sudoers temp file: %w", err)
	}

	if err := p.chmodFile(tempPath, 0440); err != nil {
		cleanupTemp()
		return "", fmt.Errorf("failed to set sudoers permissions: %w", err)
	}

	if err := p.validateSudoersFile(tempPath); err != nil {
		cleanupTemp()
		return "", err
	}

	if err := p.renameFile(tempPath, targetPath); err != nil {
		cleanupTemp()
		return "", fmt.Errorf("failed to install sudoers grant: %w", err)
	}

	if err := p.chmodFile(targetPath, 0440); err != nil {
		return "", fmt.Errorf("failed to set installed sudoers permissions: %w", err)
	}

	return targetPath, nil
}

func (p *localProvider) buildSudoersContent(username string, command []string) (string, error) {
	if strings.ContainsAny(username, " \t\r\n") {
		return "", fmt.Errorf("local username %q is not supported in sudoers grant", username)
	}

	spec := "ALL"
	if len(command) > 0 {
		resolved, err := sudoersCommandSpec(command)
		if err != nil {
			return "", err
		}
		spec = resolved
	}

	return fmt.Sprintf("# Managed by Thand\n%s ALL=(ALL:ALL) NOPASSWD: %s\n", username, spec), nil
}

func (p *localProvider) validateSudoersFile(path string) error {
	visudoPath, err := p.resolveExecutable(
		p.GetConfig().GetStringWithDefault("visudo_path", ""),
		[]string{"visudo", "/usr/sbin/visudo"},
	)
	if err != nil {
		return fmt.Errorf("failed to locate visudo for sudoers validation: %w", err)
	}

	result, runErr := p.runCommand(visudoPath, "-c", "-f", path)
	if runErr != nil {
		return fmt.Errorf("sudoers validation failed: %w\nstdout:\n%s\nstderr:\n%s", runErr, result.Stdout, result.Stderr)
	}

	return nil
}

func (p *localProvider) sudoExecutable() string {
	return p.GetConfig().GetStringWithDefault("sudo_path", "sudo")
}

func (p *localProvider) resolveCommand(command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	executable, err := p.resolveExecutable(command[0], []string{command[0]})
	if err != nil {
		return nil, fmt.Errorf("failed to locate command %q: %w", command[0], err)
	}

	resolved := append([]string{executable}, command[1:]...)
	return resolved, nil
}

func (p *localProvider) resolveExecutable(value string, fallbacks []string) (string, error) {
	candidates := []string{}
	if len(value) > 0 {
		candidates = append(candidates, value)
	}
	candidates = append(candidates, fallbacks...)

	var lastErr error
	for _, candidate := range candidates {
		if len(candidate) == 0 {
			continue
		}
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			} else {
				lastErr = err
				continue
			}
		}
		resolved, err := p.lookPath(candidate)
		if err == nil {
			return resolved, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no executable candidates provided")
	}
	return "", lastErr
}

func sudoersFragmentName(roleName string, command []string, grantID string) (string, error) {
	base := sanitizeFragmentComponent(roleName)
	if len(base) == 0 {
		base = "local-sudo"
	}
	grant := sanitizeFragmentComponent(grantID)
	if len(grant) == 0 {
		return "", fmt.Errorf("grant id is required")
	}

	if len(command) == 0 {
		return fmt.Sprintf("thand-%s-%s", base, grant), nil
	}

	hash := sha256.Sum256([]byte(strings.Join(command, "\x00")))
	return fmt.Sprintf("thand-%s-%s-%s", base, grant, hex.EncodeToString(hash[:])[:10]), nil
}

func sanitizeFragmentComponent(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func sudoersCommandSpec(command []string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("command is required")
	}

	escaped := make([]string, 0, len(command))
	for _, part := range command {
		if len(part) == 0 {
			return "", fmt.Errorf("command arguments must not be empty")
		}
		replacer := strings.NewReplacer(
			`\\`, `\\\\`,
			" ", `\ `,
			",", `\,`,
			":", `\:`,
			"=", `\=`,
		)
		escaped = append(escaped, replacer.Replace(part))
	}

	return strings.Join(escaped, " "), nil
}

func decodeLocalSudoRequestMetadata(value map[string]any) (models.LocalSudoRequestMetadata, error) {
	return models.DecodeLocalSudoRequestMetadata(value)
}

func decodeLocalSudoAuthorizationMetadata(value map[string]any) (models.LocalSudoAuthorizationMetadata, error) {
	return models.DecodeLocalSudoAuthorizationMetadata(value)
}

func runSystemCommand(name string, args ...string) (commandResult, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()

	result := commandResult{
		Stdout: string(output),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		result.Stderr = string(exitErr.Stderr)
		return result, err
	}

	if err != nil {
		return result, err
	}

	return result, nil
}

func init() {
	providers.Register(LocalProviderName, &localProvider{}, LocalCapabilities, &ConfigSchema{})
}
