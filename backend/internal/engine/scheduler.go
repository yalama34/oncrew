package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
)

const maxWorkers = 3

var errDependenciesBlocked = errors.New("[ENGINE] Tasks cannot start because dependencies are blocked")

type taskEvent struct {
	ID  uuid.UUID
	Err error
}

func (e *Engine) executeReadyTasks(ctx context.Context, run domain.Run, reviews ...func() (uuid.UUID, []agent.Action, error)) error {
	events := make(chan taskEvent, maxWorkers)
	running := make(map[uuid.UUID]uuid.UUID)
	busy := make(map[uuid.UUID]bool)
	var result error
	stopping := false
	var deferred []agent.Action
	var deferredLead uuid.UUID
	started := false

	for {
		if !stopping {
			tasks, err := e.Store.ListTasksByRun(ctx, run.ID)
			if err != nil {
				result = errors.Join(result, err)
				stopping = true
			} else {
				dependencies, err := e.Store.ListTaskDependencies(ctx, run.ID)
				if err != nil {
					result = errors.Join(result, err)
					stopping = true
				} else {
					statuses := make(map[uuid.UUID]domain.TaskStatus, len(tasks))
					waiting := make(map[uuid.UUID][]uuid.UUID)
					for _, task := range tasks {
						statuses[task.ID] = task.Status
					}
					for _, dep := range dependencies {
						waiting[dep.TaskID] = append(waiting[dep.TaskID], dep.DependsOnID)
					}
					for _, task := range tasks {
						if len(running) >= maxWorkers {
							break
						}
						if task.Status != domain.TaskStatusTodo {
							continue
						}
						if task.AssigneeID == nil {
							result = errors.Join(result, fmt.Errorf("[ENGINE] Task %s has no assignee", task.ID))
							stopping = true
							break
						}
						if busy[*task.AssigneeID] {
							continue
						}
						ready := true
						for _, id := range waiting[task.ID] {
							if statuses[id] != domain.TaskStatusDone {
								ready = false
								break
							}
						}
						if !ready {
							continue
						}
						claimed, err := e.Store.ClaimTask(ctx, task.ID)
						if err != nil {
							result = errors.Join(result, err)
							stopping = true
							break
						}
						if !claimed {
							continue
						}
						running[task.ID] = *task.AssigneeID
						started = true
						busy[*task.AssigneeID] = true
						go func(task domain.Task) {
							err := e.executeTask(ctx, run, task)
							if err != nil {
								saved, getErr := e.Store.GetTask(context.Background(), task.ID)
								if getErr == nil && saved.Status == domain.TaskStatusInProgress {
									err = e.failTask(task.ID, "Не удалось выполнить задачу. Подробности доступны в логах сервера.", err)
								}
							}
							events <- taskEvent{ID: task.ID, Err: err}
						}(task)
					}
				}
			}
		}
		if len(running) == 0 {
			if result != nil {
				return result
			}
			if len(deferred) > 0 {
				return e.applyActions(ctx, run.ID, deferredLead, deferred)
			}
			if !started {
				tasks, err := e.Store.ListTasksByRun(ctx, run.ID)
				if err != nil {
					return err
				}
				var hasTodo, hasReview bool
				for _, task := range tasks {
					switch task.Status {
					case domain.TaskStatusTodo:
						hasTodo = true
					case domain.TaskStatusReview:
						hasReview = true
					case domain.TaskStatusInProgress:
						return e.failTask(task.ID, "Выполнение прервано до получения результата.", fmt.Errorf("[ENGINE] Task %s was interrupted", task.ID))
					}
				}
				if hasTodo && !hasReview {
					return errDependenciesBlocked
				}
			}
			return nil
		}
		event := <-events
		delete(busy, running[event.ID])
		delete(running, event.ID)
		if event.Err != nil {
			result = errors.Join(result, fmt.Errorf("task %s: %w", event.ID, event.Err))
			stopping = true
			continue
		}
		if stopping || len(running) == 0 || len(reviews) == 0 || reviews[0] == nil {
			continue
		}
		leadID, actions, err := reviews[0]()
		if err != nil {
			result = errors.Join(result, err)
			stopping = true
			continue
		}
		ask := false
		for _, action := range actions {
			if action.Type == agent.ActionAskUser {
				ask = true
				break
			}
		}
		if ask {
			stopping = true
			deferred = actions
			deferredLead = leadID
			continue
		}
		if err := e.applyActions(ctx, run.ID, leadID, actions); err != nil {
			result = errors.Join(result, err)
			stopping = true
		}
	}
}

func (e *Engine) dependencyResults(ctx context.Context, runID, taskID uuid.UUID) ([]agent.DependencyResult, error) {
	dependencies, err := e.Store.ListTaskDependencies(ctx, runID)
	if err != nil {
		return nil, err
	}
	var results []agent.DependencyResult
	for _, dependency := range dependencies {
		if dependency.TaskID != taskID {
			continue
		}
		task, err := e.Store.GetTask(ctx, dependency.DependsOnID)
		if err != nil {
			return nil, err
		}
		if task.Status != domain.TaskStatusDone {
			return nil, fmt.Errorf("[ENGINE] Dependency %s is not accepted", task.ID)
		}
		artifacts, err := e.Store.ListArtifactsByTask(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		results = append(results, agent.DependencyResult{Title: task.Text, Summary: task.Summary, Artifacts: artifacts})
	}
	return results, nil
}
