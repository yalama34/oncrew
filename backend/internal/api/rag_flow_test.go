package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/engine"
	"github.com/yalama34/orchestrator/internal/rag"
	"github.com/yalama34/orchestrator/internal/store"
	"github.com/yalama34/orchestrator/internal/store/postgres"
	pg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSpecificationRAGFlow(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "rag lead", Model: "rag-lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "rag worker", Model: "rag-worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	spec := "# Доставка\nАдрес обязателен.\n# Платежи\nОплата только картой.\n# Логи\nХранить логи.\n# UI\nТёмная тема.\n# Безопасность\nИспользовать TLS.\n# Отчёты\nЭкспорт CSV."
	var leadCalls, workerCalls int
	transport := flowRoundTrip(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		prompt := input.Messages[1].Content
		if input.Model == "rag-worker" {
			workerCalls++
			if !strings.Contains(prompt, "Оплата только картой") || !strings.Contains(prompt, "spec.md > Платежи") {
				return nil, fmt.Errorf("worker did not receive matching specification")
			}
			return flowStreamResponse(`{"summary":"Оплата готова","done":true,"artifacts":[{"filename":"pay.txt","language":"text","content":"картой"}]}`), nil
		}
		leadCalls++
		if !strings.Contains(prompt, spec) {
			return nil, fmt.Errorf("lead did not receive full specification")
		}
		if leadCalls == 1 {
			return flowStreamResponse(`{"actions":[{"type":"create_tasks","tasks":[{"title":"Платежи","details":"Сделать оплату","assign_to":"backend"}]}]}`), nil
		}
		if !strings.Contains(prompt, "Требования из spec.md > Платежи") || !strings.Contains(prompt, "pay.txt") {
			return nil, fmt.Errorf("review did not receive specification or artifact")
		}
		ids := regexp.MustCompile(`id=([0-9a-f-]{36})`).FindStringSubmatch(prompt)
		if len(ids) < 2 {
			return nil, fmt.Errorf("missing task")
		}
		return flowStreamResponse(fmt.Sprintf(`{"actions":[{"type":"accept_task","task_id":%q},{"type":"complete_run","summary":"готово"}]}`, ids[1])), nil
	})
	embeddingTransport := flowRoundTrip(func(req *http.Request) (*http.Response, error) {
		var data struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
			return nil, err
		}
		type entry struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		var entries []entry
		for i, text := range data.Input {
			vector := []float64{0, 1}
			if strings.Contains(strings.ToLower(text), "плат") || strings.Contains(strings.ToLower(text), "оплат") {
				vector = []float64{1, 0}
			}
			entries = append(entries, entry{Index: i, Embedding: vector})
		}
		body, _ := json.Marshal(map[string]interface{}{"data": entries})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	emb := rag.NewEmbeddingClient(&http.Client{Transport: embeddingTransport})
	emb.Model = "test-model"
	emb.BaseURL = "http://embeddings"
	eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: transport}), &st, 5)
	eng.RAG = &rag.Service{Client: emb, Store: &st}
	run := domain.Run{ID: uuid.New(), LeadID: lead.ID, Goal: "Сделать магазин", Status: domain.RunStatusPending, SpecText: spec, SpecName: "spec.md", SpecStatus: "pending"}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunAgents(context.Background(), run.ID, []domain.Agent{worker}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background(), run.ID, run.Goal); err != nil {
		t.Fatal(err)
	}
	result, err := st.GetRun(context.Background(), run.ID)
	if err != nil || result.Status != domain.RunStatusFinished || result.SpecStatus != "ready" {
		t.Fatalf("run: %+v %v", result, err)
	}
	if leadCalls != 2 || workerCalls != 1 {
		t.Fatalf("calls: %d %d", leadCalls, workerCalls)
	}
	chunks, err := st.FindDocumentChunks(context.Background(), run.ID, "test-model", store.Vector{1, 0}, 5)
	if err != nil || len(chunks) != 5 || chunks[0].Heading != "Платежи" {
		t.Fatalf("search: %+v, %v", chunks, err)
	}
	foreign, err := st.FindDocumentChunks(context.Background(), uuid.New(), "test-model", store.Vector{1, 0}, 5)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("run isolation: %+v %v", foreign, err)
	}
}

func TestSpecificationIndexFailure(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "fail lead", Model: "test", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(context.Background(), &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), LeadID: lead.ID, Goal: "test", Status: domain.RunStatusPending, SpecText: "Требования", SpecStatus: "pending"}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	eng := engine.NewEngine(agent.NewClient(&http.Client{}), &st, 5)
	if err := eng.Start(context.Background(), run.ID, run.Goal); err == nil {
		t.Fatal("expected failure")
	}
	saved, err := st.GetRun(context.Background(), run.ID)
	if err != nil || saved.Status != domain.RunStatusFailed || saved.SpecStatus != "failed" || saved.SpecError == "" {
		t.Fatalf("run: %+v %v", saved, err)
	}
}

func TestRunRejectsOversizedSpecification(t *testing.T) {
	request := RunRequest{Goal: "test", LeadID: uuid.NewString(), AgentIDs: []string{uuid.NewString()}, SpecText: strings.Repeat("a", 128<<10+1)}
	body, _ := json.Marshal(request)
	handler := &RunHandler{}
	rec := httptest.NewRecorder()
	handler.CreateRun(rec, httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}
