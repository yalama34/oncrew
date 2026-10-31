package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/rag"
	"github.com/yalama34/orchestrator/internal/store"
)

func memoryEmbeddingTransport(fail *atomic.Bool) testTransport {
	return func(req *http.Request) (*http.Response, error) {
		if fail != nil && fail.Load() {
			return nil, fmt.Errorf("embedding unavailable")
		}
		var input struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		var items []map[string]interface{}
		for i := range input.Input {
			items = append(items, map[string]interface{}{"index": i, "embedding": []float64{1, 0}})
		}
		data, _ := json.Marshal(map[string]interface{}{"data": items})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	}
}

func waitMemory(t *testing.T, eng *Engine, id uuid.UUID, status string) domain.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := eng.Store.GetTask(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if task.MemoryStatus == status {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := eng.Store.GetTask(context.Background(), id)
	t.Fatalf("memory status %q, want %q: %v", task.MemoryStatus, status, err)
	return domain.Task{}
}

func TestAcceptedTaskMemoryReachesWorkerAndIsIsolated(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "memory lead", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "memory worker", Model: "worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Frontend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "memory test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	otherRun := domain.Run{ID: uuid.New(), Goal: "other run", LeadID: lead.ID, Status: domain.RunStatusRunning}
	for _, r := range []*domain.Run{&run, &otherRun} {
		if err := st.CreateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	accepted := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "API заказов", Details: "Формат ответа", Summary: "Заказ содержит order_token", Status: domain.TaskStatusReview}
	foreign := domain.Task{ID: uuid.New(), RunID: otherRun.ID, Text: "Другой проект", Summary: "foreign_secret_marker", Status: domain.TaskStatusReview}
	work := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "Подключить заказы", Details: "Использовать API заказов", Status: domain.TaskStatusTodo, AssigneeID: &worker.ID}
	for _, task := range []*domain.Task{&accepted, &foreign, &work} {
		if err := st.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	artifact := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: accepted.ID, Filename: "orders.go", Language: "go", Content: "type Order struct { Token string `json:\"order_token\"` }"}
	if err := st.CreateArtifact(ctx, &artifact); err != nil {
		t.Fatal(err)
	}
	var workerPrompt string
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		workerPrompt = input.Messages[1].Content
		return streamResponse(`{"summary":"готово","done":true,"artifacts":[]}`), nil
	})
	emb := rag.NewEmbeddingClient(&http.Client{Transport: memoryEmbeddingTransport(nil)})
	emb.Model = "test-model"
	emb.BaseURL = "http://embeddings"
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	eng.RAG = &rag.Service{Client: emb, Store: st}
	if sources := eng.searchTaskMemory(ctx, run.ID, work); len(sources) != 0 {
		t.Fatalf("unaccepted task in memory: %+v", sources)
	}
	for _, task := range []domain.Task{accepted, foreign} {
		if err := eng.applyActions(ctx, task.RunID, lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: task.ID.String()}}); err != nil {
			t.Fatal(err)
		}
	}
	waitMemory(t, eng, accepted.ID, "ready")
	waitMemory(t, eng, foreign.ID, "ready")
	sources := eng.searchTaskMemory(ctx, run.ID, work)
	var summary, code bool
	for _, source := range sources {
		if source.TaskID == foreign.ID.String() || strings.Contains(source.Content, "foreign_secret_marker") {
			t.Fatalf("foreign memory returned: %+v", source)
		}
		if source.Source == "результат задачи" && strings.Contains(source.Content, "order_token") {
			summary = true
		}
		if source.Filename == "orders.go" && strings.Contains(source.Content, "order_token") {
			code = true
		}
	}
	if !summary || !code {
		t.Fatalf("missing summary or artifact: %+v", sources)
	}
	if err := eng.executeReadyTasks(ctx, run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(workerPrompt, "Релевантные принятые решения") || !strings.Contains(workerPrompt, "order_token") || !strings.Contains(workerPrompt, "orders.go") {
		t.Fatalf("worker missing task memory: %s", workerPrompt)
	}
	if strings.Contains(workerPrompt, "foreign_secret_marker") {
		t.Fatal("worker received memory from another run")
	}
	leadPrompt, err := eng.buildLeadUserPrompt(ctx, run.ID, run.Goal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(leadPrompt, "Принятое решение задачи API заказов") || !strings.Contains(leadPrompt, "orders.go") {
		t.Fatalf("lead missing task memory: %s", leadPrompt)
	}
	dependency := domain.TaskDependency{TaskID: work.ID, DependsOnID: accepted.ID}
	if err := st.CreateTaskGraph(ctx, nil, []domain.TaskDependency{dependency}); err != nil {
		t.Fatal(err)
	}
	if sources := eng.searchTaskMemory(ctx, run.ID, work); len(sources) != 0 {
		t.Fatalf("direct dependency duplicated in memory: %+v", sources)
	}
}

func TestTaskMemoryRetryReplacesOldVersion(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "retry lead", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "API", Summary: "first", Status: domain.TaskStatusReview}
	if err := st.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	fail := &atomic.Bool{}
	fail.Store(true)
	emb := rag.NewEmbeddingClient(&http.Client{Transport: memoryEmbeddingTransport(fail)})
	emb.Model = "test-model"
	emb.BaseURL = "http://embeddings"
	eng := NewEngine(nil, st, 40)
	eng.RAG = &rag.Service{Client: emb, Store: st}
	if err := eng.applyActions(ctx, run.ID, lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: task.ID.String()}}); err != nil {
		t.Fatal(err)
	}
	failed := waitMemory(t, eng, task.ID, "failed")
	if failed.Status != domain.TaskStatusDone || failed.MemoryError == "" {
		t.Fatalf("acceptance lost: %+v", failed)
	}
	fail.Store(false)
	if err := eng.RetryTaskMemory(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	ready := waitMemory(t, eng, task.ID, "ready")
	if ready.MemoryVersion != failed.MemoryVersion+1 {
		t.Fatalf("unexpected version %+v", ready)
	}
	if err := st.SaveTaskMemory(ctx, task.ID, failed.MemoryVersion, nil); !errors.Is(err, store.ErrMemoryStale) {
		t.Fatalf("old version saved: %v", err)
	}
	if err := st.SetTaskResult(ctx, task.ID, "second", domain.TaskStatusReview); err != nil {
		t.Fatal(err)
	}
	if err := eng.applyActions(ctx, run.ID, lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: task.ID.String()}}); err != nil {
		t.Fatal(err)
	}
	updated := waitMemory(t, eng, task.ID, "ready")
	if updated.MemoryVersion != ready.MemoryVersion+1 {
		t.Fatalf("new acceptance not versioned: %+v", updated)
	}
	chunks, err := eng.RAG.SearchTaskMemory(ctx, run.ID, "API", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || !strings.Contains(chunks[0].Content, "second") || strings.Contains(chunks[0].Content, "first") {
		t.Fatalf("stale memory: %+v", chunks)
	}
}

func TestLiveTaskMemoryWithPostgres(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_LIVE_MEMORY_TEST") != "1" {
		t.Skip("set ORCHESTRATOR_LIVE_MEMORY_TEST=1")
	}
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "live memory lead", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "API заказов", Details: "Создать заказ", Summary: "POST /api/orders принимает имя и телефон", Status: domain.TaskStatusReview}
	if err := st.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	artifact := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: task.ID, Filename: "orders.go", Content: "type Order struct { Phone string }"}
	if err := st.CreateArtifact(ctx, &artifact); err != nil {
		t.Fatal(err)
	}
	emb := rag.NewEmbeddingClient(&http.Client{Timeout: 30 * time.Second})
	emb.BaseURL = "http://localhost:1234"
	emb.Model = "text-embedding-embeddinggemma-300m-qat"
	eng := NewEngine(nil, st, 40)
	eng.RAG = &rag.Service{Client: emb, Store: st}
	if err := eng.applyActions(ctx, run.ID, lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: task.ID.String()}}); err != nil {
		t.Fatal(err)
	}
	waitMemory(t, eng, task.ID, "ready")
	results, err := eng.RAG.SearchTaskMemory(ctx, run.ID, "Как создать заказ и указать телефон?", nil)
	if err != nil || len(results) < 2 {
		t.Fatalf("live search returned %d chunks: %v", len(results), err)
	}
	var summary, artifactFound bool
	for _, result := range results {
		if result.TaskID != task.ID || len(result.Embedding) != 768 {
			t.Fatalf("unexpected source: %+v", result)
		}
		if result.Source == "task_summary" {
			summary = true
		}
		if result.Source == "artifact" && result.Filename == "orders.go" {
			artifactFound = true
		}
	}
	if !summary || !artifactFound {
		t.Fatalf("live search missing expected sources")
	}
}

func TestPendingTaskMemoryResumes(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "resume memory lead", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusFinished}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "resume memory", Summary: "accepted", Status: domain.TaskStatusDone, MemoryStatus: "pending", MemoryVersion: 1}
	if err := st.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	emb := rag.NewEmbeddingClient(&http.Client{Transport: memoryEmbeddingTransport(nil)})
	emb.Model = "test-model"
	emb.BaseURL = "http://embeddings"
	eng := NewEngine(nil, st, 40)
	eng.RAG = &rag.Service{Client: emb, Store: st}
	if err := eng.ResumePendingTaskMemory(ctx); err != nil {
		t.Fatal(err)
	}
	waitMemory(t, eng, task.ID, "ready")
}
