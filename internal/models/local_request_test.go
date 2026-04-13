package models

import (
	"slices"
	"testing"
)

func TestNormalizeLocalSudoRequestDefaultsTargetFromEnvironment(t *testing.T) {
	request := &ElevateRequest{
		Role: &Role{
			Identifier: LocalSudoRoleIdentifier,
			Name:       "Local Sudo",
		},
		Reason:   "Routine maintenance",
		Duration: "30m",
		Metadata: LocalSudoRequestMetadata{Mode: LocalSudoModeTimed}.AsMap(),
	}

	err := NormalizeLocalSudoRequest(
		request,
		map[string]ProviderConfig{
			"local-elevation": {Provider: "local", Enabled: true},
		},
		&EnvironmentConfig{Platform: Local, Name: "test-host"},
		LocalSudoNormalizeOptions{},
	)
	if err != nil {
		t.Fatalf("NormalizeLocalSudoRequest returned error: %v", err)
	}

	if got, want := request.Workflow, LocalSudoTimedWorkflowName; got != want {
		t.Fatalf("workflow = %q, want %q", got, want)
	}
	if got, want := request.Providers[0], "local-elevation"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := request.Metadata["target_agent"], "thand_local_test_host"; got != want {
		t.Fatalf("target_agent = %#v, want %q", got, want)
	}
}

func TestNormalizeLocalSudoRequestRequiresExplicitTargetWhenConfigured(t *testing.T) {
	request := &ElevateRequest{
		Role: &Role{
			Identifier: LocalSudoRoleIdentifier,
			Name:       "Local Sudo",
		},
		Workflow: LocalSudoTimedWorkflowName,
		Reason:   "Routine maintenance",
		Duration: "30m",
		Metadata: LocalSudoRequestMetadata{Mode: LocalSudoModeTimed}.AsMap(),
	}

	err := NormalizeLocalSudoRequest(
		request,
		map[string]ProviderConfig{
			"local-elevation": {Provider: "local", Enabled: true},
		},
		&EnvironmentConfig{Platform: Local, Name: "test-host"},
		LocalSudoNormalizeOptions{RequireTarget: true},
	)
	if err == nil || err.Error() != "local sudo requires an explicit target agent" {
		t.Fatalf("expected explicit target error, got %v", err)
	}
}

func TestNormalizeLocalSudoRequestCommandDefaultsDuration(t *testing.T) {
	request := &ElevateRequest{
		Role: &Role{
			Identifier: LocalSudoRoleIdentifier,
			Name:       "Local Sudo",
		},
		Reason: "Run whoami",
		Metadata: LocalSudoRequestMetadata{
			Mode:        LocalSudoModeCommand,
			Command:     []string{"whoami"},
			TargetAgent: "thand_local_remote",
		}.AsMap(),
	}

	err := NormalizeLocalSudoRequest(
		request,
		map[string]ProviderConfig{
			"local": {Provider: "local", Enabled: true},
		},
		nil,
		LocalSudoNormalizeOptions{RequireTarget: true},
	)
	if err != nil {
		t.Fatalf("NormalizeLocalSudoRequest returned error: %v", err)
	}

	if got, want := request.Workflow, LocalSudoCommandWorkflowName; got != want {
		t.Fatalf("workflow = %q, want %q", got, want)
	}
	if got, want := request.Duration, LocalSudoCommandDuration; got != want {
		t.Fatalf("duration = %q, want %q", got, want)
	}
}

func TestNormalizeLocalSudoRequestPreservesExplicitProviderAlias(t *testing.T) {
	request := &ElevateRequest{
		Role: &Role{
			Identifier: LocalSudoRoleIdentifier,
			Name:       "Local Sudo",
			Providers:  []string{"local", "local-elevation"},
		},
		Providers: []string{"local-custom"},
		Workflow:  LocalSudoTimedWorkflowName,
		Reason:    "Routine maintenance",
		Duration:  "30m",
		Metadata: LocalSudoRequestMetadata{
			Mode:        LocalSudoModeTimed,
			TargetAgent: "thand_local_remote",
		}.AsMap(),
	}

	err := NormalizeLocalSudoRequest(
		request,
		nil,
		nil,
		LocalSudoNormalizeOptions{RequireTarget: true},
	)
	if err != nil {
		t.Fatalf("NormalizeLocalSudoRequest returned error: %v", err)
	}

	if got, want := request.Providers[0], "local-custom"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if !slices.Contains(request.Role.Providers, "local-custom") {
		t.Fatalf("role providers = %#v, want explicit provider alias preserved", request.Role.Providers)
	}
}
