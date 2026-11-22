package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
)

var errInvalidFilePlan = errors.New("invalid file plan")

func (e *Engine) validateLeadFilePlans(ctx context.Context, runID uuid.UUID, actions []agent.Action) error {
	existing, err := e.Store.ListArtifactsByRun(ctx, runID)
	if err != nil {
		return err
	}
	available := map[string]bool{}
	for _, artifact := range existing {
		available[artifact.Filename] = true
	}
	for _, action := range actions {
		if action.Type != agent.ActionCreateTasks {
			continue
		}
		byKey := map[string]agent.TaskSpec{}
		for _, spec := range action.Tasks {
			byKey[spec.Key] = spec
		}
		var producedByDeps func(string, string, map[string]bool) bool
		producedByDeps = func(key, name string, seen map[string]bool) bool {
			if seen[key] {
				return false
			}
			seen[key] = true
			spec, ok := byKey[key]
			if !ok {
				return false
			}
			for _, dep := range spec.DependsOn {
				for _, path := range byKey[dep].Writes {
					if path == name {
						return true
					}
				}
				if producedByDeps(dep, name, seen) {
					return true
				}
			}
			return false
		}
		for _, spec := range action.Tasks {
			if spec.Reads == nil || spec.Writes == nil {
				return fmt.Errorf("%w: task %q requires reads and writes arrays", errInvalidFilePlan, spec.Title)
			}
			if _, err := taskFiles(uuid.Nil, spec.Reads, spec.Writes); err != nil {
				return fmt.Errorf("%w: %v", errInvalidFilePlan, err)
			}
			for _, name := range spec.Reads {
				if !available[name] && !producedByDeps(spec.Key, name, map[string]bool{}) {
					return fmt.Errorf("%w: task %q reads missing file %q; create it in a dependency or remove it", errInvalidFilePlan, spec.Title, name)
				}
			}
		}
	}
	return nil
}

func (e *Engine) createTaskGraph(ctx context.Context, runID uuid.UUID, specs []agent.TaskSpec, findWorker func(string) (domain.Agent, error)) error {
	return e.createTaskGraphWithKeys(ctx, runID, specs, findWorker, nil)
}

func taskFiles(id uuid.UUID, reads, writes []string) ([]domain.TaskFile, error) {
	fileMap := map[string]domain.TaskFile{}
	for _, entry := range []struct {
		paths []string
		write bool
	}{{reads, false}, {writes, true}} {
		for _, name := range entry.paths {
			if name == "" || name == "." || strings.TrimSpace(name) != name || strings.Contains(name, "\\") || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
				return nil, fmt.Errorf("[ENGINE] Invalid file path %q", name)
			}
			f := fileMap[name]
			f.TaskID = id
			f.Path = name
			if entry.write {
				f.Write = true
			} else {
				f.Read = true
			}
			fileMap[name] = f
		}
	}
	files := make([]domain.TaskFile, 0, len(fileMap))
	for _, file := range fileMap {
		files = append(files, file)
	}
	return files, nil
}

func (e *Engine) createTaskGraphWithKeys(ctx context.Context, runID uuid.UUID, specs []agent.TaskSpec, findWorker func(string) (domain.Agent, error), created map[string]uuid.UUID) error {
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
		if _, ok := created[key]; ok {
			return fmt.Errorf("[ENGINE] Duplicate task key %q in lead response", key)
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
	// Preserve explicit dependencies and serialize tasks that write the same file.
	var reaches func(string, string, map[string]bool) bool
	reaches = func(from, to string, seen map[string]bool) bool {
		if from == to {
			return true
		}
		if seen[from] {
			return false
		}
		seen[from] = true
		for _, dep := range graph[from] {
			if reaches(dep, to, seen) {
				return true
			}
		}
		return false
	}
	for i := range specs {
		for j := 0; j < i; j++ {
			shared := false
			for _, a := range specs[i].Writes {
				for _, b := range specs[j].Writes {
					if a == b {
						shared = true
					}
				}
			}
			if shared && !reaches(specs[i].Key, specs[j].Key, map[string]bool{}) && !reaches(specs[j].Key, specs[i].Key, map[string]bool{}) {
				graph[specs[i].Key] = append(graph[specs[i].Key], specs[j].Key)
			}
		}
	}
	var tasks []domain.Task
	var dependencies []domain.TaskDependency
	var sources []domain.TaskArtifactSource
	var files []domain.TaskFile
	for _, spec := range specs {
		worker, err := findWorker(spec.AssignTo)
		if err != nil {
			return fmt.Errorf("[ENGINE] Failed to assign task: %w", err)
		}
		workerID := worker.ID
		details := spec.Details
		planned := spec.Reads != nil && spec.Writes != nil
		for _, artifactIDText := range spec.ArtifactIDs {
			artifactID, err := uuid.Parse(artifactIDText)
			if err != nil {
				return fmt.Errorf("[ENGINE] Invalid artifact id %q", artifactIDText)
			}
			artifact, err := e.Store.GetArtifactById(ctx, artifactID)
			if err != nil || artifact.RunID != runID {
				return fmt.Errorf("[ENGINE] Artifact %s does not belong to this run", artifactID)
			}
			if planned {
				continue
			}
			details += fmt.Sprintf("\n\nИсходный артефакт для изменения (%s, id=%s):\n%s", artifact.Filename, artifact.ID, artifact.Content)
			versions, err := e.Store.ListArtifactVersions(ctx, runID, artifact.ID)
			if err != nil {
				return err
			}
			sources = append(sources, domain.TaskArtifactSource{TaskID: ids[spec.Key], ArtifactID: artifact.ID, Version: versions[len(versions)-1].Version})
		}
		plannedFiles, err := taskFiles(ids[spec.Key], spec.Reads, spec.Writes)
		if err != nil {
			return err
		}
		files = append(files, plannedFiles...)
		tasks = append(tasks, domain.Task{ID: ids[spec.Key], RunID: runID, Status: domain.TaskStatusTodo, Text: spec.Title, Details: details, AssigneeID: &workerID, FilePlan: planned})
		for _, dependency := range graph[spec.Key] {
			dependencies = append(dependencies, domain.TaskDependency{TaskID: ids[spec.Key], DependsOnID: ids[dependency]})
		}
	}
	if err := e.Store.CreateTaskGraph(ctx, tasks, dependencies, sources, files); err != nil {
		return fmt.Errorf("[ENGINE] Failed to save task graph: %w", err)
	}
	if created != nil {
		for key, id := range ids {
			created[key] = id
		}
	}
	return nil
}
