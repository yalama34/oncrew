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
	created := make(map[string]uuid.UUID)
	findTask := func(id string) (domain.Task, error) {
		if taskID, ok := created[id]; ok {
			return e.Store.GetTask(ctx, taskID)
		}
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
			if err := e.createTaskGraphWithKeys(ctx, runID, a.Tasks, findWorker, created); err != nil {
				return err
			}
			if len(a.Tasks) > 0 {
				if err := e.Store.MarkRevisionWork(ctx, runID); err != nil {
					return err
				}
			}
		case agent.ActionAssignTask:
			task, err := findTask(a.TaskID)
			if err != nil {
				return fmt.Errorf("[ENGINE] Failed to assign task %q: %w", a.TaskID, err)
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
			if task.Status != domain.TaskStatusReview && task.Status != domain.TaskStatusBlocked && task.Status != domain.TaskStatusDone {
				return fmt.Errorf("[ENGINE] Task %s cannot be reworked", task.ID)
			}
			if strings.TrimSpace(a.Feedback) == "" {
				return fmt.Errorf("[ENGINE] Rework feedback is required")
			}
			if a.Reads != nil || a.Writes != nil {
				if a.Reads == nil || a.Writes == nil {
					return fmt.Errorf("[ENGINE] Both reads and writes are required")
				}
				files, fileErr := taskFiles(task.ID, a.Reads, a.Writes)
				if fileErr != nil {
					return fileErr
				}
				err = e.Store.ReworkTask(ctx, task.ID, a.Feedback, files)
			} else {
				err = e.Store.ReworkTask(ctx, task.ID, a.Feedback)
			}
			if err != nil {
				return err
			}
			if err := e.Store.MarkRevisionWork(ctx, runID); err != nil {
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
			if revision, err := e.Store.ActiveRevision(ctx, runID); err == nil && !revision.WorkStarted {
				return fmt.Errorf("[ENGINE] Cannot finish revision before assigning work")
			}
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
			if revision, err := e.Store.ActiveRevision(ctx, runID); err == nil {
				if err := e.Store.UpdateRevisionStatus(ctx, revision.ID, domain.RunStatusFinished, a.Summary, ""); err != nil {
					return err
				}
			}
			fmt.Println("PLACEHOLDER complete run", a.Summary)
			return nil
		default:
			return fmt.Errorf("[ENGINE] Unknown action %q", a.Type)
		}

	}
	return nil
}
