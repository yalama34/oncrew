package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/store"
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

func (h *ArtifactHandler) GetArtifactVersions(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	artifactID, err := uuid.Parse(chi.URLParam(r, "artifactID"))
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	versions, err := h.Store.ListArtifactVersions(r.Context(), runID, artifactID)
	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, "artifact not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	out := make([]ArtifactVersionDTO, 0, len(versions))
	for _, v := range versions {
		var revisionID *string
		if v.RevisionID != nil {
			value := v.RevisionID.String()
			revisionID = &value
		}
		out = append(out, ArtifactVersionDTO{ID: v.ID.String(), TaskID: v.TaskID.String(), Filename: v.Filename, Language: v.Language, Content: v.Content, Version: v.Version, RevisionID: revisionID, CreatedAt: v.CreatedAt.Format(time.RFC3339Nano)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
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
