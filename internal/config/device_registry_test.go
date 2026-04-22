package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thand-io/agent/internal/models"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

type fakeDeviceRegistryClient struct {
	describeResponse *workflowservice.DescribeWorkflowExecutionResponse
	describeErr      error
	queryResponse    *client.QueryWorkflowWithOptionsResponse
	queryErr         error
	terminated       []string
	signalOptions    []client.StartWorkflowOptions
	signalNames      []string
	signalArgs       []any
}

func (f *fakeDeviceRegistryClient) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return f.describeResponse, f.describeErr
}

func (f *fakeDeviceRegistryClient) QueryWorkflowWithOptions(ctx context.Context, request *client.QueryWorkflowWithOptionsRequest) (*client.QueryWorkflowWithOptionsResponse, error) {
	return f.queryResponse, f.queryErr
}

func (f *fakeDeviceRegistryClient) SignalWithStartWorkflow(
	ctx context.Context,
	workflowID string,
	signalName string,
	signalArg interface{},
	options client.StartWorkflowOptions,
	workflow interface{},
	args ...interface{},
) (client.WorkflowRun, error) {
	f.signalOptions = append(f.signalOptions, options)
	f.signalNames = append(f.signalNames, signalName)
	f.signalArgs = append(f.signalArgs, signalArg)
	return nil, nil
}

func (f *fakeDeviceRegistryClient) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	f.terminated = append(f.terminated, workflowID)
	return nil
}

func TestEnsureRegistryWorkflowTaskQueueTerminatesWrongQueue(t *testing.T) {
	t.Parallel()

	client := &fakeDeviceRegistryClient{
		describeResponse: &workflowservice.DescribeWorkflowExecutionResponse{
			ExecutionConfig: &workflowpb.WorkflowExecutionConfig{
				TaskQueue: &taskqueuepb.TaskQueue{Name: "thand_local_old_server"},
			},
		},
	}

	err := ensureRegistryWorkflowTaskQueue(context.Background(), client, models.TemporalDeviceRouteRegistryWorkflowID)
	require.NoError(t, err)
	assert.Equal(t, []string{models.TemporalDeviceRouteRegistryWorkflowID}, client.terminated)
}

func TestPublishDeviceDefinitionUsesCanonicalRegistryQueue(t *testing.T) {
	t.Parallel()

	client := &fakeDeviceRegistryClient{}
	err := publishDeviceDefinition(context.Background(), client, models.Device{
		ID:      "device-alpha",
		Name:    "Device Alpha",
		Enabled: true,
	})
	require.NoError(t, err)
	require.Len(t, client.signalOptions, 1)
	assert.Equal(t, models.TemporalDeviceRegistryTaskQueue, client.signalOptions[0].TaskQueue)
	assert.Equal(t, models.TemporalDeviceDefinitionUpsertSignalName, client.signalNames[0])
}

func TestQueryDeviceDefinitionReturnsStoredDevice(t *testing.T) {
	t.Parallel()

	payloads, err := converter.GetDefaultDataConverter().ToPayloads(models.Device{
		ID:      "device-alpha",
		Name:    "Device Alpha",
		Enabled: true,
	})
	require.NoError(t, err)

	client := &fakeDeviceRegistryClient{
		queryResponse: &client.QueryWorkflowWithOptionsResponse{
			QueryResult: client.NewValue(payloads),
		},
	}

	device, err := queryDeviceDefinition(context.Background(), client, "device-alpha")
	require.NoError(t, err)
	require.NotNil(t, device)
	assert.Equal(t, "device-alpha", device.ID)
	assert.Equal(t, "Device Alpha", device.Name)
}
