package models_test

import (
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/serverlessworkflow/sdk-go/v3/model"
	"github.com/thand-io/agent/internal/models"
	taskModel "github.com/thand-io/agent/internal/workflows/tasks/model"
)

func TestWorkflowClonePreservesThandAgentTasks(t *testing.T) {
	workflow := &models.Workflow{
		Version: version.Must(version.NewVersion("1.0.0")),
		Name:    "Local Sudo Timed Elevation",
		Workflow: &model.Workflow{
			Document: model.Document{
				DSL:       "1.0.0-alpha5",
				Namespace: "thand",
				Name:      "local-sudo-timed-elevation",
				Version:   "1.0.0",
			},
			Do: &model.TaskList{
				{
					Key: "authorize",
					Task: &taskModel.ThandTask{
						Thand: taskModel.ThandTypeAgent,
						With: &models.BasicConfig{
							"identities": "${ $context.metadata.target_agent }",
						},
						Do: &model.TaskList{
							{
								Key: "authorize_local",
								Task: &taskModel.ThandTask{
									Thand: taskModel.ThandTypeAuthorize,
									With: &models.BasicConfig{
										"revocation": "revoke",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cloned := workflow.GetWorkflowClone()
	if cloned == nil {
		t.Fatal("GetWorkflowClone returned nil")
	}
	if cloned.Do == nil || len(*cloned.Do) != 1 {
		t.Fatalf("cloned workflow do = %#v, want one task", cloned.Do)
	}

	authorizeTask, ok := (*cloned.Do)[0].Task.(*taskModel.ThandTask)
	if !ok {
		t.Fatalf("authorize task type = %T, want *taskModel.ThandTask", (*cloned.Do)[0].Task)
	}
	if authorizeTask.Thand != taskModel.ThandTypeAgent {
		t.Fatalf("authorize thand type = %q, want %q", authorizeTask.Thand, taskModel.ThandTypeAgent)
	}
	if authorizeTask.Do == nil || len(*authorizeTask.Do) != 1 {
		t.Fatalf("authorize do = %#v, want one nested task", authorizeTask.Do)
	}

	nestedTask, ok := (*authorizeTask.Do)[0].Task.(*taskModel.ThandTask)
	if !ok {
		t.Fatalf("nested authorize task type = %T, want *taskModel.ThandTask", (*authorizeTask.Do)[0].Task)
	}
	if nestedTask.Thand != taskModel.ThandTypeAuthorize {
		t.Fatalf("nested authorize thand type = %q, want %q", nestedTask.Thand, taskModel.ThandTypeAuthorize)
	}
}
