package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/preview"
	"github.com/yalama34/orchestrator/internal/store"
)

func (h *ArtifactHandler) CreatePreview(w http.ResponseWriter, r *http.Request) {
	if h.Preview == nil {
		http.Error(w, preview.ErrUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "некорректный запуск", http.StatusBadRequest)
		return
	}
	artifacts, err := h.Store.ListPreviewArtifacts(r.Context(), runID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "запуск не найден", http.StatusNotFound)
		case errors.Is(err, store.ErrRunNotFinished):
			http.Error(w, "дождитесь завершения запуска", http.StatusConflict)
		default:
			log.Printf("[PREVIEW] Snapshot for run %s: %v", runID, err)
			http.Error(w, "не удалось загрузить файлы запуска", http.StatusInternalServerError)
		}
		return
	}
	files := make([]preview.File, 0, len(artifacts))
	for _, artifact := range artifacts {
		files = append(files, preview.File{Path: artifact.Filename, Content: artifact.Content})
	}
	bundle, err := preview.Bundle(files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	session, err := h.Preview.Create(r.Context(), runID, bundle)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, preview.ErrCapacity) {
			status = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(session)
}

func (h *ArtifactHandler) StopPreview(w http.ResponseWriter, r *http.Request) {
	if h.Preview == nil {
		http.Error(w, preview.ErrUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}
	runID, runErr := uuid.Parse(chi.URLParam(r, "id"))
	id, sessionErr := uuid.Parse(chi.URLParam(r, "previewID"))
	if runErr != nil || sessionErr != nil {
		http.Error(w, "некорректная сессия", http.StatusBadRequest)
		return
	}
	if err := h.Preview.Stop(r.Context(), runID, id); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
