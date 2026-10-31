package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
)

func (h *RunHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	ctx := r.Context()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Goal == "" || req.LeadID == "" || len(req.AgentIDs) == 0 {
		http.Error(w, "all params are required", http.StatusBadRequest)
		return
	}
	if len(req.SpecText) > 128<<10 {
		http.Error(w, "ТЗ слишком большое: максимум 128 КБ", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SpecText) == "" {
		req.SpecText = ""
		req.SpecName = ""
	}
	if strings.TrimSpace(req.SpecText) != "" && strings.TrimSpace(req.SpecName) == "" {
		req.SpecName = "Текст ТЗ"
	}
	leadID, err := uuid.Parse(req.LeadID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	lead, err := h.Store.GetAgent(ctx, leadID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if lead.Role.Name != domain.Teamlead {
		http.Error(w, "lead must have teamlead role", http.StatusBadRequest)
		return
	}
	var agents []domain.Agent
	for _, agentStrID := range req.AgentIDs {
		agentID, err := uuid.Parse(agentStrID)
		if err != nil {
			http.Error(w, "invalid uuid", http.StatusBadRequest)
			return
		}
		agent, err := h.Store.GetAgent(ctx, agentID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		agents = append(agents, agent)
	}
	run := domain.Run{
		ID:        uuid.New(),
		Goal:      req.Goal,
		SpecText:  req.SpecText,
		SpecName:  req.SpecName,
		Status:    domain.RunStatusPending,
		LeadID:    leadID,
		Lead:      &lead,
		Agents:    agents,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if strings.TrimSpace(req.SpecText) != "" {
		run.SpecStatus = "pending"
	}
	if err := h.Store.CreateRun(ctx, &run); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	go func() {
		if err := h.Engine.Start(context.Background(), run.ID, run.Goal); err != nil {
			log.Printf("[API] Run %s failed: %v", run.ID, err)
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": run.ID.String()})
}

func (h *RunHandler) GetAllRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runs, err := h.Store.GetAllRuns(ctx)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	runsDTO := make([]RunDTO, 0, len(runs))
	for _, run := range runs {
		var agentIDs []string
		for _, agent := range run.Agents {
			agentIDs = append(agentIDs, agent.ID.String())
		}
		tasks, err := h.Store.ListTasksByRun(ctx, run.ID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		runDTO := RunDTO{
			ID:         run.ID.String(),
			Goal:       run.Goal,
			Status:     string(run.Status),
			LeadID:     run.LeadID.String(),
			AgentIDs:   agentIDs,
			CreatedAt:  run.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt:  run.UpdatedAt.Format(time.RFC3339Nano),
			TasksTotal: len(tasks),
			SpecName:   run.SpecName, SpecStatus: run.SpecStatus, SpecError: run.SpecError, FailureReason: run.FailureReason,
		}
		for _, task := range tasks {
			if task.Status == domain.TaskStatusDone {
				runDTO.TasksDone++
			}
			if task.Status == domain.TaskStatusReview {
				runDTO.TasksReview++
			}
		}
		runsDTO = append(runsDTO, runDTO)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(runsDTO)
}

func (h *RunHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runStringID := chi.URLParam(r, "id")
	runID, err := uuid.Parse(runStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	run, err := h.Store.GetRun(ctx, runID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var agentIDs []string
	for _, agent := range run.Agents {
		agentIDs = append(agentIDs, agent.ID.String())
	}
	tasks, err := h.Store.ListTasksByRun(ctx, run.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	runDTO := RunDTO{
		ID:         run.ID.String(),
		Goal:       run.Goal,
		Status:     string(run.Status),
		LeadID:     run.LeadID.String(),
		AgentIDs:   agentIDs,
		CreatedAt:  run.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:  run.UpdatedAt.Format(time.RFC3339Nano),
		TasksTotal: len(tasks),
		SpecName:   run.SpecName, SpecStatus: run.SpecStatus, SpecError: run.SpecError, FailureReason: run.FailureReason,
	}
	for _, task := range tasks {
		if task.Status == domain.TaskStatusDone {
			runDTO.TasksDone++
		}
		if task.Status == domain.TaskStatusReview {
			runDTO.TasksReview++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(runDTO)
}
