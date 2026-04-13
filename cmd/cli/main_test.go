package cli

import (
	"errors"
	"testing"

	"github.com/serverlessworkflow/sdk-go/v3/model"
	"github.com/thand-io/agent/internal/common"
	"github.com/thand-io/agent/internal/config"
	"github.com/thand-io/agent/internal/models"
)

func TestInitializeAgentModeUsesLoginServerSyncWhenNoLocalTemporalConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Login: models.LoginConfig{
			Endpoint: &model.Endpoint{
				EndpointConfig: &model.EndpointConfiguration{
					URI: &model.LiteralUri{Value: "https://login.example.com"},
				},
			},
		},
	}

	var reloadCalled, syncCalled, initCalled bool
	err := initializeAgentMode(
		cfg,
		func() error {
			reloadCalled = true
			return nil
		},
		func() error {
			syncCalled = true
			return nil
		},
		func() error {
			initCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("initializeAgentMode returned error: %v", err)
	}
	if !reloadCalled {
		t.Fatal("expected reloadConfig to be called")
	}
	if !syncCalled {
		t.Fatal("expected syncWithLoginServer to be called")
	}
	if initCalled {
		t.Fatal("expected initializeProviders to be skipped after successful sync")
	}
}

func TestInitializeAgentModeFallsBackToLocalTemporalConfigOnSyncFailure(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Login: models.LoginConfig{
			Endpoint: &model.Endpoint{
				EndpointConfig: &model.EndpointConfiguration{
					URI: &model.LiteralUri{Value: "https://login.example.com"},
				},
			},
		},
		Services: models.ServicesConfig{
			Temporal: &models.TemporalConfig{
				Host:      "localhost",
				Port:      7233,
				Namespace: "default",
			},
		},
	}

	var initCalled bool
	err := initializeAgentMode(
		cfg,
		func() error { return nil },
		func() error { return errors.New("sync failed") },
		func() error {
			initCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("initializeAgentMode returned error: %v", err)
	}
	if !initCalled {
		t.Fatal("expected initializeProviders to be called after sync fallback")
	}
}

func TestInitializeAgentModeFailsWithoutTemporalWhenSyncFails(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Login: models.LoginConfig{
			Endpoint: &model.Endpoint{
				EndpointConfig: &model.EndpointConfiguration{
					URI: &model.LiteralUri{Value: "https://login.example.com"},
				},
			},
		},
	}

	err := initializeAgentMode(
		cfg,
		func() error { return nil },
		func() error { return config.ErrNoActiveLoginSession },
		func() error { return nil },
	)
	if err == nil {
		t.Fatal("expected initializeAgentMode to fail when sync fails and no local Temporal config exists")
	}
	if !errors.Is(err, config.ErrNoActiveLoginSession) {
		t.Fatalf("expected ErrNoActiveLoginSession, got %v", err)
	}
}

func TestInitializeAgentModeUsesLocalConfigWhenLoginServerIsDefault(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Login: models.LoginConfig{
			Endpoint: &model.Endpoint{
				EndpointConfig: &model.EndpointConfiguration{
					URI: &model.LiteralUri{Value: common.DefaultLoginServerEndpoint},
				},
			},
		},
	}

	var syncCalled, initCalled bool
	err := initializeAgentMode(
		cfg,
		func() error { return nil },
		func() error {
			syncCalled = true
			return nil
		},
		func() error {
			initCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("initializeAgentMode returned error: %v", err)
	}
	if syncCalled {
		t.Fatal("expected syncWithLoginServer to be skipped for the default login server")
	}
	if !initCalled {
		t.Fatal("expected initializeProviders to be called")
	}
}
