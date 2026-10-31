package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
)

func (h *AgentHandler) GetAgentsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentsList, err := h.Store.ListAgents(ctx)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var agentsListDTO []AgentDTO
	for _, agent := range agentsList {
		agentDTO := AgentDTO{
			ID:    agent.ID.String(),
			Name:  agent.Name,
			Model: agent.Model,
			Role:  string(agent.Role.Name),
		}
		agentsListDTO = append(agentsListDTO, agentDTO)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(agentsListDTO)
}

func (h *AgentHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req AgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.APIKey == "" || req.Model == "" || req.Role == "" {
		http.Error(w, "name, model, api_ley, role are required", http.StatusBadRequest)
		return
	}
	agent := domain.Agent{
		ID:      uuid.New(),
		Name:    req.Name,
		Model:   req.Model,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		BotKey:  req.BotKey,
		Role: domain.Role{
			Name: domain.RoleName(req.Role),
		},
	}
	ctx := r.Context()
	if err := h.Store.CreateAgent(ctx, &agent); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": agent.ID.String()})
}

func (h *AgentHandler) GetAgentByID(w http.ResponseWriter, r *http.Request) {
	agentStringID := chi.URLParam(r, "id")
	agentID, err := uuid.Parse(agentStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	agent, err := h.Store.GetAgent(ctx, agentID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	agentDTO := AgentDTO{
		ID:    agent.ID.String(),
		Name:  agent.Name,
		Model: agent.Model,
		Role:  string(agent.Role.Name),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(agentDTO)
}
