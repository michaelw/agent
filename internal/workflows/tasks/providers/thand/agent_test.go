package thand

import (
	"testing"

	"github.com/serverlessworkflow/sdk-go/v3/model"
	"github.com/thand-io/agent/internal/models"
	taskModel "github.com/thand-io/agent/internal/workflows/tasks/model"
	sdkWorkflowsModel "github.com/thand-io/agent/sdk/workflows/models"
)

func TestParseIdentitiesFallsBackToRequestMetadata(t *testing.T) {
	task := &thandTask{}
	workflowTask := newTestElevateWorkflowTask(t)
	workflowTask.SetInstanceCtx(models.ElevateRequestInternal{
		ElevateRequest: models.ElevateRequest{
			Metadata: map[string]any{
				"target_agent": " thand_local_mbp05 ",
			},
		},
	})

	identities, err := task.parseIdentities(workflowTask, &taskModel.ThandTask{
		Thand: taskModel.ThandTypeAgent,
	})
	if err != nil {
		t.Fatalf("parseIdentities returned error: %v", err)
	}
	if len(identities) != 1 || identities[0] != "thand_local_mbp05" {
		t.Fatalf("identities = %#v, want [\"thand_local_mbp05\"]", identities)
	}
}

func TestMergeAgentBranchContextMergesNestedMaps(t *testing.T) {
	workflowTask := newTestElevateWorkflowTask(t)
	workflowTask.SetInstanceCtx(map[string]any{
		"authorizations": map[string]any{
			"alice@example.com": map[string]any{"user_id": "alice"},
		},
	})

	mergeAgentBranchContext(workflowTask, map[string]any{
		"approved": true,
		"authorizations": map[string]any{
			"bob@example.com": map[string]any{"user_id": "bob"},
		},
	})

	contextMap := workflowTask.GetContextAsMap()
	authorizations, ok := contextMap["authorizations"].(map[string]any)
	if !ok {
		t.Fatalf("authorizations type = %T, want map[string]any", contextMap["authorizations"])
	}
	if _, ok := authorizations["alice@example.com"]; !ok {
		t.Fatalf("merged authorizations = %#v, missing alice entry", authorizations)
	}
	if _, ok := authorizations["bob@example.com"]; !ok {
		t.Fatalf("merged authorizations = %#v, missing bob entry", authorizations)
	}
	if approved, _ := contextMap["approved"].(bool); !approved {
		t.Fatalf("approved = %#v, want true", contextMap["approved"])
	}
}

func newTestElevateWorkflowTask(t *testing.T) *models.ElevateWorkflowTask {
	t.Helper()

	workflowCtx, err := sdkWorkflowsModel.NewWorkflowContext(&model.Workflow{
		Document: model.Document{
			DSL:       "1.0.0-alpha5",
			Namespace: "thand",
			Name:      "test-workflow",
			Version:   "1.0.0",
		},
		Do: &model.TaskList{},
	})
	if err != nil {
		t.Fatalf("NewWorkflowContext returned error: %v", err)
	}

	return models.NewElevateWorkflowTask(workflowCtx)
}
