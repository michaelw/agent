package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thand-io/agent/internal/models"
	taskModel "github.com/thand-io/agent/internal/workflows/tasks/model"
)

func TestGetWorkflowFromElevationRequest_PreservesSudoAgentTasks(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		mode: ModeServer,
		Environment: models.EnvironmentConfig{
			Platform: models.Local,
		},
	}

	workflows, err := cfg.LoadWorkflows()
	require.NoError(t, err)
	cfg.Workflows.Definitions = workflows

	request := &models.ElevateRequest{
		Providers: []string{"local-elevation"},
		Workflow:  "local_sudo_timed_elevation",
		Role: &models.Role{
			Name:      "Local Sudo",
			Providers: []string{"local", "local-elevation"},
			Workflows: []string{"local_sudo_timed_elevation", "local_sudo_command_elevation"},
		},
	}

	workflowDef, err := cfg.GetWorkflowFromElevationRequest(request)
	require.NoError(t, err)
	require.NotNil(t, workflowDef)
	require.NotNil(t, workflowDef.Workflow)
	require.NotNil(t, workflowDef.Workflow.Do)

	authorizeTask := workflowDef.Workflow.Do.Key("authorize")
	require.NotNil(t, authorizeTask)

	authorizeThandTask, ok := authorizeTask.Task.(*taskModel.ThandTask)
	require.True(t, ok, "authorize task type = %T, want *taskModel.ThandTask", authorizeTask.Task)
	require.Equal(t, taskModel.ThandTypeAgent, authorizeThandTask.Thand)

	revokeTask := workflowDef.Workflow.Do.Key("revoke")
	require.NotNil(t, revokeTask)

	revokeThandTask, ok := revokeTask.Task.(*taskModel.ThandTask)
	require.True(t, ok, "revoke task type = %T, want *taskModel.ThandTask", revokeTask.Task)
	require.Equal(t, taskModel.ThandTypeAgent, revokeThandTask.Thand)
}
