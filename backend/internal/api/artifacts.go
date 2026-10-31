package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *ArtifactHandler) GetArtifacts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runStringID := chi.URLParam(r, "id")
	runID, err := uuid.Parse(runStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	artifacts, err := h.Store.ListArtifactsByRun(ctx, runID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var artifactsDTO []ArtifactDTO
	for _, artifact := range artifacts {
		artifactDTO := ArtifactDTO{
			ID:       artifact.ID.String(),
			RunID:    artifact.RunID.String(),
			TaskID:   artifact.TaskID.String(),
			Filename: artifact.Filename,
			Language: artifact.Language,
			Content:  artifact.Content,
		}
		artifactsDTO = append(artifactsDTO, artifactDTO)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(artifactsDTO)
}

func (h *ArtifactHandler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	artifactStringID := chi.URLParam(r, "id")
	artifactID, err := uuid.Parse(artifactStringID)
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	artifact, err := h.Store.GetArtifactById(ctx, artifactID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	artifactDTO := ArtifactDTO{
		ID:       artifact.ID.String(),
		RunID:    artifact.RunID.String(),
		TaskID:   artifact.TaskID.String(),
		Filename: artifact.Filename,
		Language: artifact.Language,
		Content:  artifact.Content,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(artifactDTO)
}
