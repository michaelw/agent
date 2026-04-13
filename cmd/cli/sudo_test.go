package cli

import (
	"testing"

	configpkg "github.com/thand-io/agent/internal/config"
	"github.com/thand-io/agent/internal/models"
)

func TestBuildLocalSudoElevationRequestTimed(t *testing.T) {
	previousCfg := cfg
	t.Cleanup(func() { cfg = previousCfg })

	cfg = newTestSudoConfig("local-custom")

	request, err := buildLocalSudoElevationRequest(nil, "system maintenance", "30m", "")
	if err != nil {
		t.Fatalf("buildLocalSudoElevationRequest returned error: %v", err)
	}

	if got, want := request.Workflow, models.LocalSudoTimedWorkflowName; got != want {
		t.Fatalf("workflow = %q, want %q", got, want)
	}
	if got, want := request.Duration, "30m"; got != want {
		t.Fatalf("duration = %q, want %q", got, want)
	}
	if got, want := request.Providers[0], "local-custom"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if request.Metadata["mode"] != string(models.LocalSudoModeTimed) {
		t.Fatalf("mode = %#v, want %q", request.Metadata["mode"], models.LocalSudoModeTimed)
	}
	if got, want := request.Metadata["target_agent"], "thand_local_test_host"; got != want {
		t.Fatalf("target_agent = %#v, want %q", got, want)
	}
	if !containsString(request.Role.Providers, "local-custom") {
		t.Fatalf("request role providers = %#v, want provider alias included", request.Role.Providers)
	}
}

func TestBuildLocalSudoElevationRequestCommandUsesDefaultDuration(t *testing.T) {
	previousCfg := cfg
	t.Cleanup(func() { cfg = previousCfg })

	cfg = newTestSudoConfig("local-elevation")

	request, err := buildLocalSudoElevationRequest([]string{"whoami"}, "check user", "", "thand_local_mbp05")
	if err != nil {
		t.Fatalf("buildLocalSudoElevationRequest returned error: %v", err)
	}

	if got, want := request.Workflow, models.LocalSudoCommandWorkflowName; got != want {
		t.Fatalf("workflow = %q, want %q", got, want)
	}
	if got, want := request.Duration, models.LocalSudoCommandDuration; got != want {
		t.Fatalf("duration = %q, want %q", got, want)
	}
	command, ok := request.Metadata["command"].([]string)
	if !ok {
		t.Fatalf("metadata command type = %T, want []string", request.Metadata["command"])
	}
	if len(command) != 1 || command[0] != "whoami" {
		t.Fatalf("metadata command = %#v, want [\"whoami\"]", command)
	}
	if got, want := request.Metadata["target_agent"], "thand_local_mbp05"; got != want {
		t.Fatalf("target_agent = %#v, want %q", got, want)
	}
	if got, want := request.Metadata["target_agent_explicit"], true; got != want {
		t.Fatalf("target_agent_explicit = %#v, want %v", got, want)
	}
}

func TestBuildLocalSudoElevationRequestRequiresTimedDuration(t *testing.T) {
	previousCfg := cfg
	t.Cleanup(func() { cfg = previousCfg })

	cfg = newTestSudoConfig("local-elevation")

	if _, err := buildLocalSudoElevationRequest(nil, "missing duration", "", ""); err == nil {
		t.Fatal("expected error for missing timed duration")
	}
}

func TestBuildLocalSudoElevationRequestPrefersLocalElevationProvider(t *testing.T) {
	previousCfg := cfg
	t.Cleanup(func() { cfg = previousCfg })

	cfg = &configpkg.Config{
		Providers: configpkg.ProviderDefinitionsConfig{
			Definitions: map[string]models.ProviderConfig{
				"local": {
					Name:     "Local",
					Provider: "local",
					Enabled:  true,
				},
				"local-elevation": {
					Name:     "Local Elevation",
					Provider: "local",
					Enabled:  true,
				},
			},
		},
		Roles: configpkg.RoleConfig{
			Definitions: map[string]models.Role{
				models.LocalSudoRoleIdentifier: {
					Name:       "Local Sudo",
					Identifier: models.LocalSudoRoleIdentifier,
					Providers:  []string{"local", "local-elevation"},
					Workflows:  []string{models.LocalSudoTimedWorkflowName, models.LocalSudoCommandWorkflowName},
					Permissions: models.RolePermissions{
						Allow: models.RoleStatements{
							{Operations: []string{"local:sudo:*"}},
						},
					},
				},
			},
		},
	}

	request, err := buildLocalSudoElevationRequest(nil, "system maintenance", "30m", "")
	if err != nil {
		t.Fatalf("buildLocalSudoElevationRequest returned error: %v", err)
	}

	if got, want := request.Providers[0], "local-elevation"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
}

func newTestSudoConfig(providerName string) *configpkg.Config {
	return &configpkg.Config{
		Environment: models.EnvironmentConfig{
			Platform: models.Local,
			Name:     "test-host",
		},
		Providers: configpkg.ProviderDefinitionsConfig{
			Definitions: map[string]models.ProviderConfig{
				providerName: {
					Name:     "Local",
					Provider: "local",
					Enabled:  true,
				},
			},
		},
		Roles: configpkg.RoleConfig{
			Definitions: map[string]models.Role{
				models.LocalSudoRoleIdentifier: {
					Name:       "Local Sudo",
					Identifier: models.LocalSudoRoleIdentifier,
					Providers:  []string{"local", "local-elevation"},
					Workflows:  []string{models.LocalSudoTimedWorkflowName, models.LocalSudoCommandWorkflowName},
					Permissions: models.RolePermissions{
						Allow: models.RoleStatements{
							{Operations: []string{"local:sudo:*"}},
						},
					},
				},
			},
		},
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestBuildLocalSudoElevationRequestRequiresConfiguredEnvironment(t *testing.T) {
	previousCfg := cfg
	t.Cleanup(func() { cfg = previousCfg })

	cfg = nil

	if _, err := buildLocalSudoElevationRequest(nil, "system maintenance", "30m", ""); err == nil {
		t.Fatal("expected error when config is unavailable")
	}
}
