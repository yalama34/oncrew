package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
)

func (e *Engine) applyActions(ctx context.Context, runID, leadID uuid.UUID, actions []agent.Action) error {
	workers, err := e.Store.ListAgentsByRun(ctx, runID)
	if err != nil {
		return err
	}
	findWorker := func(role string) (domain.Agent, error) {
		for _, worker := range workers {
			if worker.Role.Name == domain.RoleName(role) {
				return worker, nil
			}
		}
		return domain.Agent{}, fmt.Errorf("[ENGINE] No worker with role %q in this run", role)
	}
	findTask := func(id string) (domain.Task, error) {
		taskID, err := uuid.Parse(id)
		if err != nil {
			return domain.Task{}, err
		}
		task, err := e.Store.GetTask(ctx, taskID)
		if err != nil {
			return domain.Task{}, err
		}
		if task.RunID != runID {
			return domain.Task{}, fmt.Errorf("[ENGINE] Task %s belongs to another run", id)
		}
		return task, nil
	}
	for _, a := range actions {
		switch a.Type {
		case agent.ActionCreateTasks:
			if err := e.createTaskGraph(ctx, runID, a.Tasks, findWorker); err != nil {
				return err
			}
		case agent.ActionAssignTask:
			task, err := findTask(a.TaskID)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to parse UUID: %w", err)
			}
			assignee := a.AssignTo
			agent, err := findWorker(assignee)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to get agent: %w", err)
			}
			if err := e.Store.AssignTask(ctx, task.ID, &agent); err != nil {
				return fmt.Errorf("[ENGINE] Failed to assign task: %w", err)
			}
		case agent.ActionUpdateContext:
			runCtx := &domain.RunContext{
				RunID:     runID,
				Content:   a.ContextMD,
				UpdatedAt: time.Now(),
				UpdatedBy: &leadID,
			}
			if err := e.Store.UpsertContext(ctx, *runCtx); err != nil {
				return fmt.Errorf("[ENGINE] Failed to do UpdateContext action: %w", err)
			}
		case agent.ActionAcceptTask:
			task, err := findTask(a.TaskID)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to accept task: %w", err)
			}
			accepted, changed, err := e.Store.AcceptTask(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to accept task: %w", err)
			}
			if changed {
				e.queueTaskMemory(accepted)
			}
		case agent.ActionReworkTask:
			task, err := findTask(a.TaskID)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to rework task: %w", err)
			}
			if task.Status != domain.TaskStatusReview && task.Status != domain.TaskStatusBlocked {
				return fmt.Errorf("[ENGINE] Task %s cannot be reworked", task.ID)
			}
			if err = e.Store.SetTaskFeedback(ctx, task.ID, a.Feedback); err != nil {
				return err
			}
			if err = e.Store.UpdateTaskStatus(ctx, task.ID, domain.TaskStatusTodo); err != nil {
				return err
			}
		case agent.ActionMessageUser:
			fmt.Println("PLACEHOLDER ask user", a.Text)
		case agent.ActionAskUser:
			question := strings.TrimSpace(a.Text)
			if question == "" {
				return fmt.Errorf("[ENGINE] Empty question to user")
			}
			if err := e.Store.CreateRunQuestion(ctx, runID, question); err != nil {
				return fmt.Errorf("[ENGINE] Failed to save question: %w", err)
			}
			return errWaitingForUser
		case agent.ActionCompleteRun:
			tasks, err := e.Store.ListTasksByRun(ctx, runID)
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if task.Status != domain.TaskStatusDone {
					return fmt.Errorf("[ENGINE] Cannot finish run with unfinished tasks")
				}
			}
			if err := e.Store.UpdateRunStatus(ctx, runID, domain.RunStatusFinished); err != nil {
				return err
			}
			fmt.Println("PLACEHOLDER complete run", a.Summary)
			return nil
		default:
			return fmt.Errorf("[ENGINE] Unknown action %q", a.Type)
		}

	}
	return nil
}
