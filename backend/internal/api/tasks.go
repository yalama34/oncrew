package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
)

func taskDTO(task domain.Task, dependencies []TaskDependencyDTO) TaskDTO {
	var assignee *string
	if task.AssigneeID != nil {
		id := task.AssigneeID.String()
		assignee = &id
	}
	return TaskDTO{ID: task.ID.String(), Text: task.Text, Status: string(task.Status), Details: task.Details, Summary: task.Summary, Feedback: task.Feedback, AssigneeID: assignee, ModelPhase: task.ModelPhase, ModelStartedAt: task.ModelStartedAt, ModelLastActivityAt: task.ModelLastActivityAt, FailureReason: task.FailureReason, Dependencies: dependencies, MemoryStatus: task.MemoryStatus, MemoryError: task.MemoryError}
}

func taskDependencies(tasks []domain.Task, dependencies []domain.TaskDependency) map[uuid.UUID][]TaskDependencyDTO {
	byID := make(map[uuid.UUID]domain.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	result := make(map[uuid.UUID][]TaskDependencyDTO)
	for _, dependency := range dependencies {
		if task, ok := byID[dependency.DependsOnID]; ok {
			result[dependency.TaskID] = append(result[dependency.TaskID], TaskDependencyDTO{ID: task.ID.String(), Text: task.Text, Status: string(task.Status)})
		}
	}
	return result
}

func (h *TaskHandler) GetTasksByRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runStringID := chi.URLParam(r, "id")
	runID, err := uuid.Parse(runStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	tasksList, err := h.Store.ListTasksByRun(ctx, runID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dependencies, err := h.Store.ListTaskDependencies(ctx, runID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	byTask := taskDependencies(tasksList, dependencies)

	dtos := make([]TaskDTO, 0, len(tasksList))

	for _, task := range tasksList {
		dtos = append(dtos, taskDTO(task, byTask[task.ID]))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(dtos)
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	tStringID := chi.URLParam(r, "id")
	ctx := r.Context()
	taskID, err := uuid.Parse(tStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	task, err := h.Store.GetTask(ctx, taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	tasks, err := h.Store.ListTasksByRun(ctx, task.RunID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dependencies, err := h.Store.ListTaskDependencies(ctx, task.RunID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	response := taskDTO(task, taskDependencies(tasks, dependencies)[task.ID])
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
