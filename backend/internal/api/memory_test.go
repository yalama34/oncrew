package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/engine"
	"github.com/yalama34/orchestrator/internal/rag"
	"github.com/yalama34/orchestrator/internal/store"
	"github.com/yalama34/orchestrator/internal/store/postgres"
	pg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRetryTaskMemoryAPI(t *testing.T) {
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
	lead := domain.Agent{ID: uuid.New(), Name: "memory api lead", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(context.Background(), &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusFinished}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "retry test", Summary: "test result", Status: domain.TaskStatusDone, MemoryStatus: "failed", MemoryVersion: 1}
	if err := st.CreateTask(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	emb := rag.NewEmbeddingClient(&http.Client{Transport: flowRoundTrip(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		started <- struct{}{}
		<-release
		var data []map[string]interface{}
		for i := range input.Input {
			data = append(data, map[string]interface{}{"index": i, "embedding": []float64{1, 0}})
		}
		body, _ := json.Marshal(map[string]interface{}{"data": data})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	emb.Model = "test-model"
	emb.BaseURL = "http://embeddings"
	eng := engine.NewEngine(nil, &st, 40)
	eng.RAG = &rag.Service{Client: emb, Store: &st}
	router := NewRouter(&AgentHandler{Store: &st}, &RunHandler{Engine: eng, Store: &st}, &TaskHandler{Store: &st}, &ArtifactHandler{Store: &st})
	path := "/api/tasks/" + task.ID.String() + "/memory/retry"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	if rec.Code != http.StatusAccepted {
		close(release)
		t.Fatalf("retry status %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("indexing did not start")
	}
	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, path, nil))
	if duplicate.Code != http.StatusConflict {
		close(release)
		t.Fatalf("duplicate retry status %d", duplicate.Code)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		saved, err := st.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.MemoryStatus == "ready" {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String(), nil))
			var dto TaskDTO
			if err := json.Unmarshal(response.Body.Bytes(), &dto); err != nil || dto.MemoryStatus != "ready" {
				t.Fatalf("response %s: %v", response.Body.String(), err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task memory was not indexed")
}
