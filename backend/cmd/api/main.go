package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/api"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/engine"
	"github.com/yalama34/orchestrator/internal/rag"
	"github.com/yalama34/orchestrator/internal/store"
	"github.com/yalama34/orchestrator/internal/store/postgres"
)

func main() {
	ctx := context.Background()

	db, err := postgres.CreateDB()
	if err != nil {
		log.Fatalf("[MAIN] Failed to create db: %v", err)
	}

	st := store.NewStore(db)
	httpClient := &http.Client{}
	if os.Getenv("LLM_INSECURE_TLS") == "true" {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		httpClient.Transport = transport
		log.Println("[MAIN] TLS certificate verification disabled for LLM requests")
	}
	client := agent.NewClient(httpClient)

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		log.Fatal("[MAIN] LLM_API_KEY is not set")
	}
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.ai.mai.ru/"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "DeepSeek-V4-Flash"
	}

	leadAgent, err := st.GetAgentByRole(ctx, "teamlead")
	if err != nil {
		log.Println("[MAIN] Lead agent not found, creating default")
		leadAgent = domain.Agent{
			ID:      uuid.New(),
			Name:    "lead",
			Model:   model,
			BaseURL: baseURL,
			APIKey:  apiKey,
			Role: domain.Role{
				Name:         domain.Teamlead,
				SystemPrompt: agent.LeadSystemPrompt(),
			},
		}
		if err := st.CreateAgent(ctx, &leadAgent); err != nil {
			log.Fatalf("[MAIN] Failed to create default lead agent: %v", err)
		}
	}

	maxLeadSteps := 40
	if value := os.Getenv("MAX_LEAD_STEPS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			log.Fatal("[MAIN] MAX_LEAD_STEPS must be a positive integer")
		}
		maxLeadSteps = parsed
	}
	eng := engine.NewEngine(client, &st, maxLeadSteps)
	embeddingClient := rag.NewEmbeddingClient(&http.Client{Timeout: 2 * time.Minute})
	embeddingClient.BaseURL = os.Getenv("EMBEDDING_BASE_URL")
	if embeddingClient.BaseURL == "" {
		embeddingClient.BaseURL = "http://localhost:1234"
	}
	embeddingClient.Model = os.Getenv("EMBEDDING_MODEL")
	embeddingClient.APIKey = os.Getenv("EMBEDDING_API_KEY")
	eng.RAG = &rag.Service{Client: embeddingClient, Store: &st}
	if err := eng.ResumePendingTaskMemory(ctx); err != nil {
		log.Printf("[MAIN] Failed to resume task memory indexing: %v", err)
	}

	agentHandler := &api.AgentHandler{Store: &st}
	runHandler := &api.RunHandler{Store: &st, Engine: eng}
	taskHandler := &api.TaskHandler{Store: &st}
	artifactHandler := &api.ArtifactHandler{Store: &st}

	router := api.NewRouter(agentHandler, runHandler, taskHandler, artifactHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("[MAIN] Server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("[MAIN] Server failed: %v", err)
	}
}
