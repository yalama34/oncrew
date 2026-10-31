package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/store"
)

func (h *RunHandler) RetryTaskMemory(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	if err := h.Engine.RetryTaskMemory(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "task not found", http.StatusNotFound)
		case errors.Is(err, store.ErrMemoryNotRetryable):
			http.Error(w, "task memory is not available for retry", http.StatusConflict)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}
