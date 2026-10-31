package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/store"
)

func (h *RunHandler) GetQuestions(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	if _, err := h.Store.GetRun(r.Context(), runID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	questions, err := h.Store.ListRunQuestions(r.Context(), runID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dtos := make([]QuestionDTO, 0, len(questions))
	for _, question := range questions {
		var answeredAt *string
		if question.AnsweredAt != nil {
			value := question.AnsweredAt.Format(time.RFC3339Nano)
			answeredAt = &value
		}
		dtos = append(dtos, QuestionDTO{ID: question.ID.String(), Question: question.Question, Answer: question.Answer, CreatedAt: question.CreatedAt.Format(time.RFC3339Nano), AnsweredAt: answeredAt})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(dtos)
}

func (h *RunHandler) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid uuid", http.StatusBadRequest)
		return
	}
	var req AnswerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	questionID, err := uuid.Parse(req.QuestionID)
	if err != nil || strings.TrimSpace(req.Answer) == "" {
		http.Error(w, "question_id and answer are required", http.StatusBadRequest)
		return
	}
	run, err := h.Store.AnswerRunQuestion(r.Context(), runID, questionID, strings.TrimSpace(req.Answer))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
		} else if errors.Is(err, store.ErrRunNotWaiting) {
			http.Error(w, "run is not waiting for this question", http.StatusConflict)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	go func() {
		if err := h.Engine.Start(context.Background(), run.ID, run.Goal); err != nil {
			log.Printf("[API] Run %s failed: %v", run.ID, err)
		}
	}()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": string(run.Status)})
}
