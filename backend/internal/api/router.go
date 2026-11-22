package api

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewRouter(ah *AgentHandler, rh *RunHandler, th *TaskHandler, arth *ArtifactHandler) http.Handler {
	r := chi.NewRouter()
	appOrigin := os.Getenv("ONCREW_ORIGIN")
	if appOrigin == "" {
		appOrigin = "http://localhost:3000"
	}

	r.Use(protectBrowserWrites(appOrigin))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{appOrigin},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
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
		r.Get("/runs/{id}/revisions", rh.GetRevisions)
		r.Post("/runs/{id}/revisions", rh.CreateRevision)
		r.Post("/runs/{id}/answers", rh.AnswerQuestion)
		r.Get("/runs/{id}/tasks", th.GetTasksByRun)
		r.Get("/tasks/{id}", th.GetTask)
		r.Post("/tasks/{id}/memory/retry", rh.RetryTaskMemory)
		r.Get("/runs/{id}/artifacts", arth.GetArtifacts)
		r.Get("/runs/{id}/artifacts/{artifactID}/versions", arth.GetArtifactVersions)
		r.Post("/runs/{id}/previews", arth.CreatePreview)
		r.Delete("/runs/{id}/previews/{previewID}", arth.StopPreview)
		r.Get("/artifacts/{id}", arth.GetArtifact)
	})
	return r
}

func protectBrowserWrites(appOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Frame-Options", "DENY")
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				origin := r.Header.Get("Origin")
				if origin != appOrigin && (origin != "" || r.Header.Get("Sec-Fetch-Site") == "cross-site") {
					http.Error(w, "origin not allowed", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
