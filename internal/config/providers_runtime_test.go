package config

import (
	"fmt"
	"testing"

	"github.com/nexus-rpc/sdk-go/nexus"
	"github.com/stretchr/testify/require"
	"github.com/thand-io/agent/internal/models"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type fakeWorker struct {
	workflows  map[string]int
	activities map[string]int
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{
		workflows:  make(map[string]int),
		activities: make(map[string]int),
	}
}

func (w *fakeWorker) RegisterWorkflow(f interface{}) {
	w.RegisterWorkflowWithOptions(f, workflow.RegisterOptions{})
}

func (w *fakeWorker) RegisterWorkflowWithOptions(_ interface{}, options workflow.RegisterOptions) {
	name := options.Name
	if name == "" {
		name = "<unnamed-workflow>"
	}
	if _, exists := w.workflows[name]; exists {
		panic(fmt.Sprintf("workflow name %q is already registered", name))
	}
	w.workflows[name] = 1
}

func (w *fakeWorker) RegisterDynamicWorkflow(_ interface{}, _ workflow.DynamicRegisterOptions) {}

func (w *fakeWorker) RegisterActivity(a interface{}) {
	w.RegisterActivityWithOptions(a, activity.RegisterOptions{})
}

func (w *fakeWorker) RegisterActivityWithOptions(_ interface{}, options activity.RegisterOptions) {
	name := options.Name
	if name == "" {
		name = "<unnamed-activity>"
	}
	if _, exists := w.activities[name]; exists {
		panic(fmt.Sprintf("activity name %q is already registered", name))
	}
	w.activities[name] = 1
}

func (w *fakeWorker) RegisterDynamicActivity(_ interface{}, _ activity.DynamicRegisterOptions) {}

func (w *fakeWorker) RegisterNexusService(_ *nexus.Service) {}

func (w *fakeWorker) Start() error { return nil }

func (w *fakeWorker) Run(_ <-chan interface{}) error { return nil }

func (w *fakeWorker) Stop() {}

type fakeTemporal struct {
	worker    worker.Worker
	taskQueue string
}

func (t *fakeTemporal) Initialize() error { return nil }

func (t *fakeTemporal) Shutdown() error { return nil }

func (t *fakeTemporal) GetClient() client.Client { return nil }

func (t *fakeTemporal) HasClient() bool { return false }

func (t *fakeTemporal) GetWorker(_ ...string) worker.Worker { return t.worker }

func (t *fakeTemporal) HasWorker() bool { return t.worker != nil }

func (t *fakeTemporal) GetHostPort() string { return "" }

func (t *fakeTemporal) GetNamespace() string { return "" }

func (t *fakeTemporal) GetTaskQueue() string { return t.taskQueue }

func (t *fakeTemporal) IsVersioningDisabled() bool { return false }

type fakeServicesClient struct {
	temporal models.TemporalImpl
}

func (s *fakeServicesClient) Initialize() error { return nil }

func (s *fakeServicesClient) Shutdown() error { return nil }

func (s *fakeServicesClient) GetAnalytics() models.Analytics { return nil }

func (s *fakeServicesClient) HasAnalytics() bool { return false }

func (s *fakeServicesClient) GetEncryption() models.EncryptionImpl { return nil }

func (s *fakeServicesClient) HasEncryption() bool { return false }

func (s *fakeServicesClient) GetVault() models.VaultImpl { return nil }

func (s *fakeServicesClient) HasVault() bool { return false }

func (s *fakeServicesClient) GetStorage() models.StorageImpl { return nil }

func (s *fakeServicesClient) HasStorage() bool { return false }

func (s *fakeServicesClient) GetScheduler() models.SchedulerImpl { return nil }

func (s *fakeServicesClient) HasScheduler() bool { return false }

func (s *fakeServicesClient) GetLargeLanguageModel() models.LargeLanguageModelImpl { return nil }

func (s *fakeServicesClient) HasLargeLanguageModel() bool { return false }

func (s *fakeServicesClient) GetTemporal() models.TemporalImpl { return s.temporal }

func (s *fakeServicesClient) HasTemporal() bool { return s.temporal != nil }

func (s *fakeServicesClient) ReloadAnalytics() error { return nil }

func (s *fakeServicesClient) ReloadEncryption() error { return nil }

func (s *fakeServicesClient) ReloadVault() error { return nil }

func (s *fakeServicesClient) ReloadScheduler() error { return nil }

func (s *fakeServicesClient) ReloadLargeLanguageModel() error { return nil }

func (s *fakeServicesClient) ReloadPublicKeyInfrastructure() error { return nil }

func (s *fakeServicesClient) ReloadTemporal() error { return nil }

func newLocalProviderForTemporalRegistration(t *testing.T, identifier string) models.Provider {
	t.Helper()

	cfg := &Config{mode: ModeAgent}
	provider, err := cfg.initializeSingleProvider(identifier, &models.ProviderConfig{
		Name:     "Local Elevation",
		Provider: "local",
		Enabled:  true,
	})
	require.NoError(t, err)
	provider.SetReady()
	return provider
}

func TestEnsureProviderTemporalBindingsSkipsAlreadyRegisteredProvider(t *testing.T) {
	t.Parallel()

	fakeTemporalClient := &fakeTemporal{
		worker:    newFakeWorker(),
		taskQueue: "thand_local_mbp05",
	}
	provider := newLocalProviderForTemporalRegistration(t, "local-elevation")

	cfg := &Config{
		mode: ModeAgent,
	}
	cfg.servicesClient = &fakeServicesClient{temporal: fakeTemporalClient}
	cfg.AddProvider("local-elevation", provider)

	require.NoError(t, cfg.registerProviderTemporalBindings("local-elevation", provider))

	registeredWorkflowCount := len(fakeTemporalClient.worker.(*fakeWorker).workflows)
	registeredActivityCount := len(fakeTemporalClient.worker.(*fakeWorker).activities)

	require.NoError(t, cfg.EnsureProviderTemporalBindings())
	require.Equal(t, registeredWorkflowCount, len(fakeTemporalClient.worker.(*fakeWorker).workflows))
	require.Equal(t, registeredActivityCount, len(fakeTemporalClient.worker.(*fakeWorker).activities))
}

func TestResetTemporalProviderBindingsClearsRegistrationCache(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.markTemporalProviderBindingRegistered("local-elevation")
	require.True(t, cfg.hasTemporalProviderBindingRegistered("local-elevation"))

	cfg.ResetTemporalProviderBindings()

	require.False(t, cfg.hasTemporalProviderBindingRegistered("local-elevation"))
}
