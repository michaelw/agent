package thand

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/thand-io/agent/internal/models"
	taskModel "github.com/thand-io/agent/internal/workflows/tasks/model"
	sdkWorkflowsModel "github.com/thand-io/agent/sdk/workflows/models"
	"github.com/thand-io/agent/sdk/workflows/runner"
	"go.temporal.io/sdk/workflow"
)

const ThandAgentTask = "agent"

// agentBranchResult holds the output from a single identity branch execution.
type agentBranchResult struct {
	identity string
	result   any
	context  map[string]any
	err      error
}

// executeAgentTask executes a do task list for each identity in parallel,
// routing activities to the task queue identified by each identity string.
// Results are returned as map[string]any keyed by identity.
func (t *thandTask) executeAgentTask(
	workflowTask *models.ElevateWorkflowTask,
	taskName string,
	call *taskModel.ThandTask,
	input any,
) (any, error) {

	log := logrus.WithField("task", taskName)

	if call.Do == nil || len(*call.Do) == 0 {
		return nil, fmt.Errorf("agent task requires a 'do' block with sub-tasks")
	}

	if !workflowTask.HasTemporalContext() {
		return nil, fmt.Errorf("agent task requires a Temporal workflow context")
	}

	// Extract identities from the interpolated with config
	identities, err := t.parseIdentities(workflowTask, call)
	if err != nil {
		return nil, err
	}

	if len(identities) == 0 {
		return nil, fmt.Errorf("agent task did not resolve any identities")
	}

	log.WithFields(logrus.Fields{
		"identities":         identities,
		"current_task_queue": workflowTask.GetTaskQueue(),
	}).Info("Executing agent task for identities")

	return t.executeAgentTemporal(workflowTask, taskName, call, identities, input)
}

// parseIdentities extracts the identities list from the task's With config.
func (t *thandTask) parseIdentities(workflowTask *models.ElevateWorkflowTask, call *taskModel.ThandTask) ([]string, error) {

	var raw any
	if call.With != nil {
		raw = (*call.With)["identities"]
	}

	identities, err := normalizeAgentIdentities(raw)
	if err != nil {
		return nil, err
	}
	if len(identities) > 0 {
		return identities, nil
	}

	elevateRequest, err := workflowTask.GetContextAsElevationRequest()
	if err != nil {
		return nil, fmt.Errorf("agent task requires 'with.identities' or request metadata target_agent: %w", err)
	}

	targetAgent, _ := elevateRequest.Metadata["target_agent"].(string)
	targetAgent = strings.TrimSpace(targetAgent)
	if len(targetAgent) == 0 {
		return nil, fmt.Errorf("agent task requires 'with.identities' or request metadata target_agent")
	}

	return []string{targetAgent}, nil
}

func normalizeAgentIdentities(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return trimIdentities(v)
	case []any:
		identities := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("identity must be a string, got %T", item)
			}
			identities = append(identities, s)
		}
		return trimIdentities(identities)
	case string:
		return trimIdentities([]string{v})
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("identities must be a string or list of strings, got %T", raw)
	}
}

func trimIdentities(values []string) ([]string, error) {
	identities := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) == 0 {
			continue
		}
		identities = append(identities, value)
	}
	if len(identities) == 0 {
		return nil, nil
	}
	return identities, nil
}

// executeAgentTemporal runs the do task list for each identity in parallel using Temporal coroutines.
// Each branch clones the workflow context and sets the task queue to the identity.
func (t *thandTask) executeAgentTemporal(
	workflowTask *models.ElevateWorkflowTask,
	taskName string,
	call *taskModel.ThandTask,
	identities []string,
	input any,
) (any, error) {

	ctx := workflowTask.GetTemporalContext()
	if ctx == nil {
		return nil, fmt.Errorf("failed to get temporal context for agent task %s", taskName)
	}

	resultCh := workflow.NewChannel(ctx)

	for _, identity := range identities {
		identity := identity // capture loop variable

		workflow.Go(ctx, func(gCtx workflow.Context) {
			// Clone the workflow task for this branch
			clonedWF := workflowTask.Clone()

			childWF, ok := clonedWF.(sdkWorkflowsModel.WorkflowTaskSupport)
			if !ok {
				resultCh.Send(gCtx, agentBranchResult{
					identity: identity,
					err:      fmt.Errorf("failed to cast cloned workflow task"),
				})
				return
			}

			// Set task queue to the identity and temporal context
			childWF.SetTaskQueue(identity)
			childWF = childWF.WithTemporalContext(gCtx)

			logrus.WithFields(logrus.Fields{
				"identity":          identity,
				"parent_task_queue": workflowTask.GetTaskQueue(),
				"target_task_queue": identity,
				"workflow_id":       workflowTask.GetWorkflowID(),
				"agent_branch_task": taskName,
			}).Info("Dispatching agent task branch to target queue")

			// Create a runner for this branch
			branchRunner := runner.NewResumableWorkflowRunner(
				t.workflowConfig.CreateRunner(childWF),
			)

			// Execute the do task list
			out, err := branchRunner.ExecuteTaskList(call.Do, input)

			resultCh.Send(gCtx, agentBranchResult{
				identity: identity,
				result:   out,
				context:  childWF.GetContextAsMap(),
				err:      err,
			})
		})
	}

	// Collect results keyed by identity
	results := make(map[string]any, len(identities))
	var failedIdentities []string
	failureDetails := make(map[string]error)

	for range identities {
		var result agentBranchResult
		resultCh.Receive(ctx, &result)

		if result.err != nil {
			logrus.WithError(result.err).
				WithField("identity", result.identity).
				Error("Agent task branch failed")
			failedIdentities = append(failedIdentities, result.identity)
			failureDetails[result.identity] = result.err
			continue
		}

		mergeAgentBranchContext(workflowTask, result.context)
		results[result.identity] = result.result
	}

	// Handle failures based on success/failure ratio
	if len(failedIdentities) > 0 {
		if len(results) == 0 {
			// All branches failed
			return nil, fmt.Errorf("all agent task branches failed for identities %v: %v",
				failedIdentities, failureDetails)
		}

		// Partial failure - some succeeded, some failed
		logrus.WithFields(logrus.Fields{
			"succeeded":         len(results),
			"failed":            len(failedIdentities),
			"failed_identities": failedIdentities,
		}).Warn("Agent task completed with partial failures")

		return map[string]any{
				taskName: results,
			}, fmt.Errorf("partial failure: %d/%d identities failed (%v): %v",
				len(failedIdentities), len(identities), failedIdentities, failureDetails)
	}

	// All branches succeeded
	return map[string]any{
		taskName: results,
	}, nil
}

func mergeAgentBranchContext(workflowTask *models.ElevateWorkflowTask, branchContext map[string]any) {
	if len(branchContext) == 0 {
		return
	}

	parentContext := workflowTask.GetContextAsMap()
	if len(parentContext) == 0 {
		parentContext = map[string]any{}
	}

	for key, value := range branchContext {
		parentContext[key] = mergeAgentContextValue(parentContext[key], value)
	}

	workflowTask.SetInstanceCtx(parentContext)
}

func mergeAgentContextValue(existing any, incoming any) any {
	existingMap, existingOK := existing.(map[string]any)
	incomingMap, incomingOK := incoming.(map[string]any)
	if !existingOK || !incomingOK {
		return incoming
	}

	merged := make(map[string]any, len(existingMap)+len(incomingMap))
	for key, value := range existingMap {
		merged[key] = value
	}
	for key, value := range incomingMap {
		merged[key] = mergeAgentContextValue(merged[key], value)
	}

	return merged
}
