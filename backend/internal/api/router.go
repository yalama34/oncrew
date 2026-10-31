package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouter(ah *AgentHandler, rh *RunHandler, th *TaskHandler, arth *ArtifactHandler) http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/agents", ah.GetAgentsList)
		r.Post("/agents", ah.CreateAgent)
		r.Get("/agents/{id}", ah.GetAgentByID)
		r.Post("/runs", rh.CreateRun)
		r.Get("/runs", rh.GetAllRuns)
		r.Get("/runs/{id}", rh.GetRun)
		r.Get("/runs/{id}/questions", rh.GetQuestions)
		r.Post("/runs/{id}/answers", rh.AnswerQuestion)
		r.Get("/runs/{id}/tasks", th.GetTasksByRun)
		r.Get("/tasks/{id}", th.GetTask)
		r.Post("/tasks/{id}/memory/retry", rh.RetryTaskMemory)
		r.Get("/runs/{id}/artifacts", arth.GetArtifacts)
		r.Get("/artifacts/{id}", arth.GetArtifact)
	})
	return r
}
