package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thand-io/agent/internal/models"
	"go.temporal.io/sdk/testsuite"
)

func TestDeviceDefinitionRegistryWorkflowReturnsConfiguredDevice(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(models.TemporalDeviceDefinitionUpsertSignalName, models.Device{
			ID:      "device-alpha",
			Name:    "Device Alpha",
			Enabled: true,
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(models.TemporalGetDeviceDefinitionQueryName, "device-alpha")
		require.NoError(t, err)

		var device models.Device
		require.NoError(t, value.Get(&device))
		assert.Equal(t, "device-alpha", device.ID)
		assert.Equal(t, "Device Alpha", device.Name)

		env.CancelWorkflow()
	}, time.Millisecond)

	env.ExecuteWorkflow(deviceDefinitionRegistryWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestDeviceDefinitionRegistryWorkflowRejectsConflictingUpdates(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(models.TemporalDeviceDefinitionUpsertSignalName, models.Device{
			ID:      "device-alpha",
			Name:    "Device Alpha",
			Enabled: true,
		})
		env.SignalWorkflow(models.TemporalDeviceDefinitionUpsertSignalName, models.Device{
			ID:      "device-alpha",
			Name:    "Conflicting Device Alpha",
			Enabled: true,
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(models.TemporalGetDeviceDefinitionQueryName, "device-alpha")
		require.NoError(t, err)

		var device models.Device
		require.NoError(t, value.Get(&device))
		assert.Equal(t, "Device Alpha", device.Name)

		env.CancelWorkflow()
	}, time.Millisecond)

	env.ExecuteWorkflow(deviceDefinitionRegistryWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
