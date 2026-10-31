package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
)

func (e *Engine) createTaskGraph(ctx context.Context, runID uuid.UUID, specs []agent.TaskSpec, findWorker func(string) (domain.Agent, error)) error {
	ids := make(map[string]uuid.UUID, len(specs))
	for i := range specs {
		key := strings.TrimSpace(specs[i].Key)
		specs[i].Key = key
		if key == "" {
			if len(specs[i].DependsOn) > 0 {
				return fmt.Errorf("[ENGINE] Task with dependencies requires a key")
			}
			key = fmt.Sprintf("_task_%d", i)
			specs[i].Key = key
		}
		if _, ok := ids[key]; ok {
			return fmt.Errorf("[ENGINE] Duplicate task key %q", key)
		}
		if strings.TrimSpace(specs[i].Title) == "" {
			return fmt.Errorf("[ENGINE] Task title is required")
		}
		ids[key] = uuid.New()
	}
	graph := make(map[string][]string, len(specs))
	for _, spec := range specs {
		seen := make(map[string]bool)
		for _, dependency := range spec.DependsOn {
			if _, ok := ids[dependency]; !ok {
				return fmt.Errorf("[ENGINE] Unknown dependency %q", dependency)
			}
			if seen[dependency] {
				return fmt.Errorf("[ENGINE] Duplicate dependency %q", dependency)
			}
			seen[dependency] = true
			graph[spec.Key] = append(graph[spec.Key], dependency)
		}
	}
	visited := make(map[string]int)
	var visit func(string) error
	visit = func(key string) error {
		if visited[key] == 1 {
			return fmt.Errorf("[ENGINE] Cyclic task dependencies")
		}
		if visited[key] == 2 {
			return nil
		}
		visited[key] = 1
		for _, dep := range graph[key] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[key] = 2
		return nil
	}
	for key := range ids {
		if err := visit(key); err != nil {
			return err
		}
	}
	var tasks []domain.Task
	var dependencies []domain.TaskDependency
	for _, spec := range specs {
		worker, err := findWorker(spec.AssignTo)
		if err != nil {
			return fmt.Errorf("[ENGINE] Failed to assign task: %w", err)
		}
		workerID := worker.ID
		tasks = append(tasks, domain.Task{ID: ids[spec.Key], RunID: runID, Status: domain.TaskStatusTodo, Text: spec.Title, Details: spec.Details, AssigneeID: &workerID})
		for _, dependency := range spec.DependsOn {
			dependencies = append(dependencies, domain.TaskDependency{TaskID: ids[spec.Key], DependsOnID: ids[dependency]})
		}
	}
	if err := e.Store.CreateTaskGraph(ctx, tasks, dependencies); err != nil {
		return fmt.Errorf("[ENGINE] Failed to save task graph: %w", err)
	}
	return nil
}
