package local

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thand-io/agent/internal/models"
)

func TestAuthorizeRoleUnixTimedCreatesAndRevokesGrant(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-1",
	})

	response, err := provider.AuthorizeRole(nil, req)
	if err != nil {
		t.Fatalf("AuthorizeRole returned error: %v", err)
	}

	sudoersPath, _ := response.Metadata["sudoers_path"].(string)
	if len(sudoersPath) == 0 {
		t.Fatal("expected sudoers_path metadata to be set")
	}
	if got := response.Metadata["grant_id"]; got != "grant-1" {
		t.Fatalf("grant_id = %#v, want %q", got, "grant-1")
	}

	content, err := os.ReadFile(sudoersPath)
	if err != nil {
		t.Fatalf("failed to read sudoers grant: %v", err)
	}
	if !strings.Contains(string(content), "tester ALL=(ALL:ALL) NOPASSWD: ALL") {
		t.Fatalf("unexpected sudoers content: %s", string(content))
	}

	if _, err := provider.RevokeRole(nil, &models.RevokeRoleRequest{
		AuthorizeRoleResponse: response,
	}); err != nil {
		t.Fatalf("RevokeRole returned error: %v", err)
	}

	if _, err := os.Stat(sudoersPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected sudoers file to be removed, stat err=%v", err)
	}
}

func TestAuthorizeRoleUnixCommandRunsThroughSudoAndCleansUp(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)

	var calls []string
	provider.runCommand = func(name string, args ...string) (commandResult, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch filepath.Base(name) {
		case "visudo":
			return commandResult{}, nil
		case "sudo":
			return commandResult{Stdout: "root\n"}, nil
		default:
			return commandResult{}, nil
		}
	}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeCommand,
		Command: []string{"whoami"},
		GrantID: "grant-2",
	})

	response, err := provider.AuthorizeRole(nil, req)
	if err != nil {
		t.Fatalf("AuthorizeRole returned error: %v", err)
	}

	if response.Metadata["stdout"] != "root\n" {
		t.Fatalf("stdout = %#v, want %q", response.Metadata["stdout"], "root\n")
	}
	if _, exists := response.Metadata["sudoers_path"]; exists {
		t.Fatalf("sudoers_path metadata should be cleared after immediate cleanup: %#v", response.Metadata["sudoers_path"])
	}
	if len(calls) < 2 {
		t.Fatalf("expected visudo and sudo invocations, got %#v", calls)
	}
	if !strings.Contains(calls[len(calls)-1], "/usr/bin/whoami") {
		t.Fatalf("expected sudo invocation to use resolved command path, got %#v", calls[len(calls)-1])
	}
}

func TestAuthorizeRoleWindowsTimedIsUnsupported(t *testing.T) {
	provider := newTestLocalProvider(t, "windows", t.TempDir())

	_, err := provider.AuthorizeRole(nil, newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-3",
	}))
	if err == nil || !strings.Contains(err.Error(), "not supported on Windows") {
		t.Fatalf("expected unsupported timed Windows error, got %v", err)
	}
}

func TestAuthorizeRoleWindowsCommandRequiresWindowsSudo(t *testing.T) {
	provider := newTestLocalProvider(t, "windows", t.TempDir())
	provider.lookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}

	_, err := provider.AuthorizeRole(nil, newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeCommand,
		Command: []string{"netstat", "-ab"},
		GrantID: "grant-4",
	}))
	if err == nil || !strings.Contains(err.Error(), "Windows Sudo is unavailable") {
		t.Fatalf("expected Windows Sudo availability error, got %v", err)
	}
}

func TestAuthorizeRoleUnixUsesTrustedIdentityUsernameFallback(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-fallback-user",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}
	provider.lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000"}, nil
	}

	response, err := provider.AuthorizeRole(nil, req)
	if err != nil {
		t.Fatalf("expected trusted identity username fallback to work, got %v", err)
	}

	if got := response.Metadata["username"]; got != "fallbackuser" {
		t.Fatalf("username metadata = %#v, want %q", got, "fallbackuser")
	}
}

func TestAuthorizeRoleUnixConfiguredUsernameOverridesIdentityUsername(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-config-wins",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}

	response, err := provider.AuthorizeRole(nil, req)
	if err != nil {
		t.Fatalf("AuthorizeRole returned error: %v", err)
	}

	if got := response.Metadata["username"]; got != "tester" {
		t.Fatalf("username metadata = %#v, want %q", got, "tester")
	}
}

func TestAuthorizeRoleUnixFailsForUnresolvableConfiguredUsername(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	(*provider.GetConfig())["username"] = "ghost"
	provider.lookupUser = func(string) (*user.User, error) {
		return nil, os.ErrNotExist
	}

	_, err := provider.AuthorizeRole(nil, newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-unresolvable-user",
	}))
	if err == nil || !strings.Contains(err.Error(), "could not be resolved") {
		t.Fatalf("expected unresolvable username error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsForUntrustedIdentityUsernameProviderID(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-untrusted-provider-id",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "saml"
	req.Identity.Providers = map[string]string{"oauth2-other": "oauth2"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "provider ids") {
		t.Fatalf("expected untrusted provider id error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsWhenTrustedProviderIDsAreUnset(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-unset-trusted-provider-ids",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "provider ids") {
		t.Fatalf("expected missing trusted provider ids error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsForMissingIdentityUsername(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-missing-identity-user",
	})
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected missing username error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsWhenIdentityHasNoProviderIDs(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-missing-provider-ids",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = nil

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "no trusted provider ids") {
		t.Fatalf("expected missing provider ids error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsWhenOnlyProviderTypeWouldHaveMatched(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2"}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-provider-type-only",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "provider ids") {
		t.Fatalf("expected provider id mismatch error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsForDeniedUsername(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-denied-user",
	})
	req.Identity.User.Username = "root"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denied username error, got %v", err)
	}
}

func TestAuthorizeRoleUnixFailsForUIDOutsideAllowedRange(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)
	delete(*provider.GetConfig(), "username")
	(*provider.GetConfig())["allowed_uid_ranges"] = []string{"1000-60000"}
	provider.lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "999"}, nil
	}

	req := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-uid-outside-range",
	})
	req.Identity.User.Username = "fallbackuser"
	req.Identity.User.Source = "oauth2"
	req.Identity.Providers = map[string]string{"oauth2-jumpcloud": "oauth2"}
	(*provider.GetConfig())["trusted_username_sources"] = []string{"oauth2-jumpcloud"}

	_, err := provider.AuthorizeRole(nil, req)
	if err == nil || !strings.Contains(err.Error(), "outside allowed UID ranges") {
		t.Fatalf("expected UID allow-range error, got %v", err)
	}
}

func TestAllowedUIDRangesFromLoginDefs(t *testing.T) {
	provider := newTestLocalProvider(t, "linux", t.TempDir())
	provider.readFile = func(string) ([]byte, error) {
		return []byte(`
# Comment
UID_MIN 1000
UID_MAX 60000
`), nil
	}

	ranges, err := provider.allowedUIDRangesFromLoginDefs()
	if err != nil {
		t.Fatalf("allowedUIDRangesFromLoginDefs returned error: %v", err)
	}

	expected := []uidRange{{Min: 1000, Max: 60000}}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("ranges = %#v, want %#v", ranges, expected)
	}
}

func TestAllowedUIDRangesFromLoginDefsFallsBackOnMalformedFile(t *testing.T) {
	provider := newTestLocalProvider(t, "linux", t.TempDir())
	provider.readFile = func(string) ([]byte, error) {
		return []byte("UID_MIN nope\nUID_MAX 500"), nil
	}

	ranges, err := provider.allowedUIDRanges()
	if err != nil {
		t.Fatalf("allowedUIDRanges returned error: %v", err)
	}

	expected := []uidRange{{Min: 1000, Max: 60000}}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("ranges = %#v, want %#v", ranges, expected)
	}
}

func TestAllowedUIDRangesConfigOverridesLoginDefs(t *testing.T) {
	provider := newTestLocalProvider(t, "linux", t.TempDir())
	(*provider.GetConfig())["allowed_uid_ranges"] = []string{"2000-2999"}
	provider.readFile = func(string) ([]byte, error) {
		return []byte("UID_MIN 1000\nUID_MAX 60000\n"), nil
	}

	ranges, err := provider.allowedUIDRanges()
	if err != nil {
		t.Fatalf("allowedUIDRanges returned error: %v", err)
	}

	expected := []uidRange{{Min: 2000, Max: 2999}}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("ranges = %#v, want %#v", ranges, expected)
	}
}

func TestAuthorizeRoleUnixTimedOverlappingGrantsUseDifferentFragments(t *testing.T) {
	tempDir := t.TempDir()
	provider := newTestLocalProvider(t, "linux", tempDir)

	reqA := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-a",
	})
	reqB := newAuthorizeRoleRequest(models.LocalSudoRequestMetadata{
		Mode:    models.LocalSudoModeTimed,
		GrantID: "grant-b",
	})

	respA, err := provider.AuthorizeRole(nil, reqA)
	if err != nil {
		t.Fatalf("AuthorizeRole A returned error: %v", err)
	}
	respB, err := provider.AuthorizeRole(nil, reqB)
	if err != nil {
		t.Fatalf("AuthorizeRole B returned error: %v", err)
	}

	pathA, _ := respA.Metadata["sudoers_path"].(string)
	pathB, _ := respB.Metadata["sudoers_path"].(string)
	if pathA == "" || pathB == "" {
		t.Fatalf("expected both sudoers paths, got %q and %q", pathA, pathB)
	}
	if pathA == pathB {
		t.Fatalf("expected different sudoers paths, both were %q", pathA)
	}

	if _, err := provider.RevokeRole(nil, &models.RevokeRoleRequest{AuthorizeRoleResponse: respA}); err != nil {
		t.Fatalf("RevokeRole A returned error: %v", err)
	}
	if _, err := os.Stat(pathA); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected first sudoers file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("expected second sudoers file to remain, stat err=%v", err)
	}

	if _, err := provider.RevokeRole(nil, &models.RevokeRoleRequest{AuthorizeRoleResponse: respB}); err != nil {
		t.Fatalf("RevokeRole B returned error: %v", err)
	}
	if _, err := os.Stat(pathB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected second sudoers file to be removed, stat err=%v", err)
	}
}

func newTestLocalProvider(t *testing.T, goos, sudoersDir string) *localProvider {
	t.Helper()

	config := models.BasicConfig{
		"username":    "tester",
		"sudoers_dir": sudoersDir,
		"visudo_path": "visudo",
		"sudo_path":   "sudo",
	}

	provider := &localProvider{}
	if err := provider.Initialize("local", models.ProviderConfig{
		Name:     "Local",
		Provider: "local",
		Enabled:  true,
		Config:   &config,
	}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	provider.goos = func() string { return goos }
	provider.lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000"}, nil
	}
	provider.readFile = func(string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	provider.lookPath = func(name string) (string, error) {
		switch name {
		case "visudo":
			return "/usr/sbin/visudo", nil
		case "sudo":
			return "/usr/bin/sudo", nil
		case "whoami":
			return "/usr/bin/whoami", nil
		case "netstat":
			return "C:\\Windows\\System32\\netstat.exe", nil
		default:
			return "", os.ErrNotExist
		}
	}
	provider.runCommand = func(name string, args ...string) (commandResult, error) {
		return commandResult{}, nil
	}

	return provider
}

func newAuthorizeRoleRequest(metadata models.LocalSudoRequestMetadata) *models.AuthorizeRoleRequest {
	return &models.AuthorizeRoleRequest{
		Identity: &models.Identity{
			User: &models.User{
				Email:    "user@example.com",
				Username: "",
				Source:   "",
			},
		},
		Role: &models.CompositeRole{
			Role: models.Role{
				Name:       "Local Sudo",
				Identifier: "local_sudo",
			},
		},
		Metadata: metadata.AsMap(),
	}
}
