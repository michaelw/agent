package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/serverlessworkflow/sdk-go/v3/model"
	"github.com/sirupsen/logrus"
)

// Validatable is an interface for tasks that support custom validation.
// Tasks implementing this interface will have their Validate method called
// during workflow validation.
type Validatable interface {
	Validate() error
}

type Workflow struct {
	Version     *version.Version `json:"version,omitempty"`
	Identifier  string           `json:"identifier"` // To be set by the system
	Name        string           `json:"name" validate:"required,min=1,max=100"`
	Description string           `json:"description" validate:"max=500"`
	Workflow    *model.Workflow  `json:"workflow,omitempty" validate:"required"`
	Enabled     bool             `json:"enabled" default:"true"` // By default enable the workflow
}

func NewWorkflow(version *version.Version, identifier string, name string, description string, workflow *model.Workflow) *Workflow {
	return &Workflow{
		Version:     version,
		Identifier:  identifier,
		Name:        name,
		Description: description,
		Workflow:    workflow,
		Enabled:     true,
	}
}

func (w *Workflow) GetVersion() *version.Version {
	return w.Version
}

func (r *Workflow) HasPermission(user *User) bool {
	return true
}

func (w *Workflow) GetIdentifier() string {
	return w.Identifier
}

func (w *Workflow) GetName() string {
	return w.Name
}

func (w *Workflow) GetDescription() string {
	return w.Description
}

func (w *Workflow) GetWorkflow() *model.Workflow {
	return w.Workflow
}

func (w *Workflow) Validate() error {

	if len(w.Name) == 0 {
		return fmt.Errorf("workflow is missing required field 'name'")
	}

	// Validate individual tasks within the workflow
	if w.Workflow != nil && w.Workflow.Do != nil {
		if err := validateTaskList(w.GetName(), *w.Workflow.Do); err != nil {
			return err
		}
	}

	return nil

}

// validateTaskList validates all tasks in a task list
func validateTaskList(workflowKey string, tasks model.TaskList) error {
	for _, taskItem := range tasks {

		if taskItem == nil || taskItem.Task == nil {
			continue
		}

		// Check if the task implements Validatable interface
		if validatable, ok := taskItem.Task.(Validatable); ok {
			if err := validatable.Validate(); err != nil {
				return fmt.Errorf("workflow '%s' task '%s': %w", workflowKey, taskItem.Key, err)
			}
		}

		// Recursively validate nested task lists (e.g., in DoTask, TryTask, etc.)
		if err := validateNestedTasks(workflowKey, taskItem); err != nil {
			return err
		}
	}
	return nil
}

// validateNestedTasks validates tasks nested within other tasks (e.g., do, try/catch blocks)
func validateNestedTasks(workflowKey string, taskItem *model.TaskItem) error {
	if taskItem == nil || taskItem.Task == nil {
		return nil
	}

	// Check for DoTask which contains a nested Do list
	if doTask, ok := taskItem.Task.(*model.DoTask); ok && doTask.Do != nil {
		if err := validateTaskList(workflowKey, *doTask.Do); err != nil {
			return err
		}
	}

	// Check for TryTask which contains Try and Catch blocks
	if tryTask, ok := taskItem.Task.(*model.TryTask); ok {
		if tryTask.Try != nil {
			if err := validateTaskList(workflowKey, *tryTask.Try); err != nil {
				return err
			}
		}
		if tryTask.Catch != nil && tryTask.Catch.Do != nil {
			if err := validateTaskList(workflowKey, *tryTask.Catch.Do); err != nil {
				return err
			}
		}
	}

	// Check for ForkTask which contains branches
	if forkTask, ok := taskItem.Task.(*model.ForkTask); ok && forkTask.Fork.Branches != nil {
		if err := validateTaskList(workflowKey, *forkTask.Fork.Branches); err != nil {
			return err
		}
	}

	return nil
}

// Create a clone of the workflow to avoid mutations
func (w *Workflow) GetWorkflowClone() *model.Workflow {
	if w.Workflow == nil {
		logrus.Errorln("Failed to clone workflow. Base workflow not provided")
		return nil
	}

	data, err := json.Marshal(w.Workflow)
	if err != nil {
		logrus.WithError(err).Errorln("Failed to marshal workflow for cloning")
		return nil
	}

	clone := &model.Workflow{}
	if err := json.Unmarshal(data, clone); err != nil {
		logrus.WithError(err).Errorln("Failed to unmarshal workflow for cloning")
		return nil
	}

	clonedTasks, err := cloneTaskList(w.Workflow.Do)
	if err != nil {
		logrus.WithError(err).Errorln("Failed to clone workflow tasks")
		return nil
	}
	clone.Do = clonedTasks

	return clone
}

func cloneTaskList(taskList *model.TaskList) (*model.TaskList, error) {
	if taskList == nil {
		return nil, nil
	}

	cloned := make(model.TaskList, 0, len(*taskList))
	for _, item := range *taskList {
		clonedItem, err := cloneTaskItem(item)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, clonedItem)
	}

	return &cloned, nil
}

func cloneTaskItem(item *model.TaskItem) (*model.TaskItem, error) {
	if item == nil {
		return nil, nil
	}
	if item.Task == nil {
		return &model.TaskItem{Key: item.Key}, nil
	}

	clonedTask, err := cloneTask(item.Task)
	if err != nil {
		return nil, fmt.Errorf("clone task %q: %w", item.Key, err)
	}

	return &model.TaskItem{
		Key:  item.Key,
		Task: clonedTask,
	}, nil
}

func cloneTask(task model.Task) (model.Task, error) {
	taskType := reflect.TypeOf(task)
	if taskType == nil {
		return nil, fmt.Errorf("task type is nil")
	}

	taskValue := reflect.New(taskType.Elem()).Interface()
	clonedTask, ok := taskValue.(model.Task)
	if !ok {
		return nil, fmt.Errorf("cloned task does not implement model.Task: %T", taskValue)
	}

	data, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}
	if err := json.Unmarshal(data, clonedTask); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}

	switch original := task.(type) {
	case *model.DoTask:
		clonedTask.(*model.DoTask).Do, err = cloneTaskList(original.Do)
	case *model.ForTask:
		clonedTask.(*model.ForTask).Do, err = cloneTaskList(original.Do)
	case *model.TryTask:
		clonedTry := clonedTask.(*model.TryTask)
		clonedTry.Try, err = cloneTaskList(original.Try)
		if err == nil && original.Catch != nil {
			if clonedTry.Catch == nil {
				clonedTry.Catch = &model.TryTaskCatch{}
			}
			clonedTry.Catch.Do, err = cloneTaskList(original.Catch.Do)
		}
	case *model.ForkTask:
		clonedTask.(*model.ForkTask).Fork.Branches, err = cloneTaskList(original.Fork.Branches)
	default:
		err = cloneTaskNestedFields(reflect.ValueOf(task), reflect.ValueOf(clonedTask))
	}
	if err != nil {
		return nil, err
	}

	return clonedTask, nil
}

func cloneTaskNestedFields(original reflect.Value, cloned reflect.Value) error {
	if !original.IsValid() || !cloned.IsValid() {
		return nil
	}
	if original.Kind() == reflect.Pointer {
		if original.IsNil() || cloned.IsNil() {
			return nil
		}
		return cloneTaskNestedFields(original.Elem(), cloned.Elem())
	}
	if original.Kind() != reflect.Struct || cloned.Kind() != reflect.Struct {
		return nil
	}

	if err := cloneTaskListField(original, cloned, "Do"); err != nil {
		return err
	}
	if err := cloneTaskListField(original, cloned, "Try"); err != nil {
		return err
	}
	if err := cloneForkBranchesField(original, cloned); err != nil {
		return err
	}
	if err := cloneCatchDoField(original, cloned); err != nil {
		return err
	}

	return nil
}

func cloneTaskListField(original reflect.Value, cloned reflect.Value, fieldName string) error {
	originalField := original.FieldByName(fieldName)
	clonedField := cloned.FieldByName(fieldName)
	if !originalField.IsValid() || !clonedField.IsValid() || !clonedField.CanSet() {
		return nil
	}
	if originalField.Type() != reflect.TypeOf((*model.TaskList)(nil)) {
		return nil
	}

	if originalField.IsNil() {
		clonedField.Set(reflect.Zero(clonedField.Type()))
		return nil
	}

	clonedTaskList, err := cloneTaskList(originalField.Interface().(*model.TaskList))
	if err != nil {
		return err
	}
	clonedField.Set(reflect.ValueOf(clonedTaskList))
	return nil
}

func cloneForkBranchesField(original reflect.Value, cloned reflect.Value) error {
	originalFork := original.FieldByName("Fork")
	clonedFork := cloned.FieldByName("Fork")
	if !originalFork.IsValid() || !clonedFork.IsValid() {
		return nil
	}

	originalBranches := originalFork.FieldByName("Branches")
	clonedBranches := clonedFork.FieldByName("Branches")
	if !originalBranches.IsValid() || !clonedBranches.IsValid() || !clonedBranches.CanSet() {
		return nil
	}
	if originalBranches.Type() != reflect.TypeOf((*model.TaskList)(nil)) {
		return nil
	}

	if originalBranches.IsNil() {
		clonedBranches.Set(reflect.Zero(clonedBranches.Type()))
		return nil
	}

	clonedTaskList, err := cloneTaskList(originalBranches.Interface().(*model.TaskList))
	if err != nil {
		return err
	}
	clonedBranches.Set(reflect.ValueOf(clonedTaskList))
	return nil
}

func cloneCatchDoField(original reflect.Value, cloned reflect.Value) error {
	originalCatch := original.FieldByName("Catch")
	clonedCatch := cloned.FieldByName("Catch")
	if !originalCatch.IsValid() || !clonedCatch.IsValid() {
		return nil
	}
	if originalCatch.IsNil() {
		return nil
	}
	if clonedCatch.IsNil() {
		clonedCatch.Set(reflect.New(clonedCatch.Type().Elem()))
	}

	originalDo := originalCatch.Elem().FieldByName("Do")
	clonedDo := clonedCatch.Elem().FieldByName("Do")
	if !originalDo.IsValid() || !clonedDo.IsValid() || !clonedDo.CanSet() {
		return nil
	}
	if originalDo.Type() != reflect.TypeOf((*model.TaskList)(nil)) {
		return nil
	}
	if originalDo.IsNil() {
		clonedDo.Set(reflect.Zero(clonedDo.Type()))
		return nil
	}

	clonedTaskList, err := cloneTaskList(originalDo.Interface().(*model.TaskList))
	if err != nil {
		return err
	}
	clonedDo.Set(reflect.ValueOf(clonedTaskList))
	return nil
}

func (w *Workflow) GetEnabled() bool {
	return w.Enabled
}

type WorkflowRequest struct {
	Task *ElevateWorkflowTask `json:"task"`
	Url  string               `json:"url"`
}

func (r *WorkflowRequest) GetTask() *ElevateWorkflowTask {
	return r.Task
}

func (r *WorkflowRequest) GetRedirectURL() string {
	return r.Url
}

type WorkflowExecutionInfo struct {
	WorkflowID string `json:"id"`
	RunID      string `json:"run"`

	StartTime time.Time  `json:"started_at"`
	CloseTime *time.Time `json:"finished_at"`

	Status string `json:"status"`
	Task   string `json:"task,omitempty"`

	History []string `json:"history,omitempty"` // History of state transitions

	// SearchAttributes are the custom search attributes associated with the workflow
	Workflow string `json:"name"` // workflowName
	Role     string `json:"role"`
	User     string `json:"user"`
	Reason   string `json:"reason,omitempty"`
	Duration int64  `json:"duration,omitempty"` // Duration in seconds
	Approved *bool  `json:"approved"`           // nil = pending approval, true = approved, false = denied

	Providers  []string    `json:"providers,omitempty"`
	Identities []*Identity `json:"identities,omitempty"`

	// Context
	Input   any `json:"input,omitempty"`
	Output  any `json:"output,omitempty"`
	Context any `json:"context,omitempty"`
}

// TaskHandler defines the signature for task execution functions
type TaskHandler func(
	workflowTask *ElevateWorkflowTask,
	task *model.TaskItem,
	input any,
) (any, error)

func (w *WorkflowExecutionInfo) GetAuthorizationTime() *time.Time {

	if w.Approved == nil {
		return nil
	}

	if !*w.Approved {
		return nil
	}

	approvalTime := time.Now()

	// Find the authorization time in the context
	if w.Context == nil {
		return &approvalTime
	}

	contextMap, ok := w.Context.(map[string]any)
	if !ok {
		return &approvalTime
	}

	if authTimeRaw, exists := contextMap["authorized_at"]; exists {
		if authTimeStr, ok := authTimeRaw.(string); ok {
			parsedTime, err := time.Parse(time.RFC3339, authTimeStr)
			if err == nil {
				return &parsedTime
			}
		}
	}

	return &approvalTime
}
