package environment

import (
	"testing"

	"github.com/thand-io/agent/internal/models"
	taskModel "github.com/thand-io/agent/internal/workflows/tasks/model"
)

func TestLocalSudoDefaultWorkflowsUseAgentRouting(t *testing.T) {
	defaults, err := GetDefaultWorkflows(models.Local)
	if err != nil {
		t.Fatalf("GetDefaultWorkflows returned error: %v", err)
	}
	if len(defaults) != 1 {
		t.Fatalf("default workflow definitions = %d, want 1", len(defaults))
	}

	timedWorkflow, ok := defaults[0].Workflows["local_sudo_timed_elevation"]
	if !ok {
		t.Fatal("local_sudo_timed_elevation workflow not found")
	}
	if timedWorkflow.Workflow == nil || timedWorkflow.Workflow.Do == nil {
		t.Fatal("timed workflow definition is incomplete")
	}

	authorizeTask := timedWorkflow.Workflow.Do.Key("authorize")
	if authorizeTask == nil {
		t.Fatal("authorize task not found")
	}

	thandAuthorizeTask, ok := authorizeTask.Task.(*taskModel.ThandTask)
	if !ok {
		t.Fatalf("authorize task type = %T, want *taskModel.ThandTask", authorizeTask.Task)
	}
	if thandAuthorizeTask.Thand != taskModel.ThandTypeAgent {
		t.Fatalf("authorize thand type = %q, want %q", thandAuthorizeTask.Thand, taskModel.ThandTypeAgent)
	}
	if thandAuthorizeTask.Do == nil || len(*thandAuthorizeTask.Do) != 1 {
		t.Fatalf("authorize agent do = %#v, want one nested task", thandAuthorizeTask.Do)
	}

	nestedAuthorize := (*thandAuthorizeTask.Do)[0]
	nestedThandTask, ok := nestedAuthorize.Task.(*taskModel.ThandTask)
	if !ok {
		t.Fatalf("nested authorize task type = %T, want *taskModel.ThandTask", nestedAuthorize.Task)
	}
	if nestedThandTask.Thand != taskModel.ThandTypeAuthorize {
		t.Fatalf("nested authorize thand type = %q, want %q", nestedThandTask.Thand, taskModel.ThandTypeAuthorize)
	}
}
