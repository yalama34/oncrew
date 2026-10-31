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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
	"github.com/yalama34/orchestrator/internal/store/postgres"
	pg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func streamResponse(content string) *http.Response {
	payload, _ := json.Marshal(map[string]interface{}{"choices": []interface{}{map[string]interface{}{"delta": map[string]string{"content": content}, "finish_reason": "stop"}}})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\ndata: [DONE]\n\n"))}
}
func testStore(t *testing.T) *store.Store {
	t.Helper()
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
	return &st
}
func TestParallelAndDependencies(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "scheduler lead", Role: domain.Role{Name: domain.Teamlead}}
	front := domain.Agent{ID: uuid.New(), Name: "scheduler front", Model: "front", BaseURL: "http://mock", Role: domain.Role{Name: domain.Frontend}}
	back := domain.Agent{ID: uuid.New(), Name: "scheduler back", Model: "back", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &front, &back} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 3)
	release := make(chan struct{})
	var integrationPrompt string
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		if strings.Contains(input.Messages[1].Content, "integration task") {
			integrationPrompt = input.Messages[1].Content
			started <- "integration"
			return streamResponse(`{"summary":"integrated","done":true,"artifacts":[]}`), nil
		}
		started <- input.Model
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		result := fmt.Sprintf(`{"summary":%q,"done":true,"artifacts":[{"filename":%q,"language":"text","content":%q}]}`, input.Model+" summary", input.Model+".txt", input.Model+" content")
		return streamResponse(result), nil
	})
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	find := func(role string) (domain.Agent, error) {
		if role == "frontend" {
			return front, nil
		}
		return back, nil
	}
	specs := []agent.TaskSpec{{Key: "api", Title: "api task", AssignTo: "backend"}, {Key: "ui", Title: "ui task", AssignTo: "frontend"}, {Key: "join", Title: "integration task", AssignTo: "frontend", DependsOn: []string{"api", "ui"}}}
	if err := eng.createTaskGraph(ctx, run.ID, specs, find); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- eng.executeReadyTasks(ctx, run) }()
	for i := 0; i < 2; i++ {
		select {
		case value := <-started:
			if value == "integration" {
				t.Fatal("dependent task started early")
			}
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("workers did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasksByRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Text == "integration task" {
			if task.Status != domain.TaskStatusTodo {
				t.Fatal("dependent task started before review")
			}
			continue
		}
		if task.Status != domain.TaskStatusReview {
			t.Fatalf("status %s", task.Status)
		}
		if err := st.UpdateTaskStatus(ctx, task.ID, domain.TaskStatusDone); err != nil {
			t.Fatal(err)
		}
	}
	if err := eng.executeReadyTasks(ctx, run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(integrationPrompt, "back content") || !strings.Contains(integrationPrompt, "front content") {
		t.Fatalf("missing dependency artifacts: %s", integrationPrompt)
	}
}
func TestInvalidGraphDoesNotCreateTasks(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	eng := NewEngine(nil, st, 40)
	runID := uuid.New()
	worker := domain.Agent{ID: uuid.New()}
	find := func(string) (domain.Agent, error) { return worker, nil }
	for _, specs := range [][]agent.TaskSpec{
		{{Key: "a", Title: "A", DependsOn: []string{"missing"}}},
		{{Key: "a", Title: "A", DependsOn: []string{"b"}}, {Key: "b", Title: "B", DependsOn: []string{"a"}}},
		{{Key: "a", Title: "A"}, {Key: "a", Title: "B"}},
	} {
		if err := eng.createTaskGraph(ctx, runID, specs, find); err == nil {
			t.Fatal("expected invalid graph error")
		}
	}
	tasks, err := st.ListTasksByRun(ctx, runID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("unexpected tasks: %v %v", tasks, err)
	}
}

func TestReviewUnblocksTaskWhileAnotherWorkerRuns(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "early lead", Role: domain.Role{Name: domain.Teamlead}}
	fast := domain.Agent{ID: uuid.New(), Name: "early front", Model: "fast", BaseURL: "http://mock", Role: domain.Role{Name: domain.Frontend}}
	slow := domain.Agent{ID: uuid.New(), Name: "early back", Model: "slow", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &fast, &slow} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 3)
	releaseFast := make(chan struct{})
	releaseSlow := make(chan struct{})
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		if strings.Contains(input.Messages[1].Content, "followup task") {
			started <- "followup"
			return streamResponse(`{"summary":"followup","done":true,"artifacts":[]}`), nil
		}
		started <- input.Model
		if input.Model == "fast" {
			<-releaseFast
		} else {
			<-releaseSlow
		}
		return streamResponse(`{"summary":"ready","done":true,"artifacts":[]}`), nil
	})
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	find := func(role string) (domain.Agent, error) {
		if role == "frontend" {
			return fast, nil
		}
		return slow, nil
	}
	specs := []agent.TaskSpec{{Key: "fast", Title: "fast task", AssignTo: "frontend"}, {Key: "slow", Title: "slow task", AssignTo: "backend"}, {Key: "follow", Title: "followup task", AssignTo: "frontend", DependsOn: []string{"fast"}}}
	if err := eng.createTaskGraph(ctx, run.ID, specs, find); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasksByRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var fastID uuid.UUID
	for _, task := range tasks {
		if task.Text == "fast task" {
			fastID = task.ID
		}
	}
	reviewCalls := 0
	review := func() (uuid.UUID, []agent.Action, error) {
		reviewCalls++
		if reviewCalls > 1 {
			return lead.ID, nil, nil
		}
		return lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: fastID.String()}}, nil
	}
	done := make(chan error, 1)
	go func() { done <- eng.executeReadyTasks(ctx, run, review) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(releaseFast)
			close(releaseSlow)
			t.Fatal("workers did not start")
		}
	}
	close(releaseFast)
	select {
	case name := <-started:
		if name != "followup" {
			t.Fatalf("unexpected %s", name)
		}
	case <-time.After(5 * time.Second):
		close(releaseSlow)
		t.Fatal("dependency did not unblock while slow task was running")
	}
	close(releaseSlow)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestQuestionWaitsForRunningWorkers(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "question lead", Role: domain.Role{Name: domain.Teamlead}}
	front := domain.Agent{ID: uuid.New(), Name: "question front", Model: "fast", BaseURL: "http://mock", Role: domain.Role{Name: domain.Frontend}}
	back := domain.Agent{ID: uuid.New(), Name: "question back", Model: "slow", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &front, &back} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	fastRelease := make(chan struct{})
	slowRelease := make(chan struct{})
	asked := make(chan struct{}, 1)
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		started <- input.Model
		if input.Model == "fast" {
			<-fastRelease
		} else {
			<-slowRelease
		}
		return streamResponse(`{"summary":"ready","done":true,"artifacts":[]}`), nil
	})
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	find := func(role string) (domain.Agent, error) {
		if role == "frontend" {
			return front, nil
		}
		return back, nil
	}
	specs := []agent.TaskSpec{{Key: "fast", Title: "fast", AssignTo: "frontend"}, {Key: "slow", Title: "slow", AssignTo: "backend"}}
	if err := eng.createTaskGraph(ctx, run.ID, specs, find); err != nil {
		t.Fatal(err)
	}
	review := func() (uuid.UUID, []agent.Action, error) {
		asked <- struct{}{}
		return lead.ID, []agent.Action{{Type: agent.ActionAskUser, Text: "Уточнение?"}}, nil
	}
	done := make(chan error, 1)
	go func() { done <- eng.executeReadyTasks(ctx, run, review) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(fastRelease)
			close(slowRelease)
			t.Fatal("workers did not start")
		}
	}
	close(fastRelease)
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		close(slowRelease)
		t.Fatal("lead did not ask")
	}
	before, err := st.GetRun(ctx, run.ID)
	if err != nil || before.Status != domain.RunStatusRunning {
		close(slowRelease)
		t.Fatalf("run before drain: %+v %v", before, err)
	}
	close(slowRelease)
	if err := <-done; !errors.Is(err, errWaitingForUser) {
		t.Fatalf("expected waiting: %v", err)
	}
	after, err := st.GetRun(ctx, run.ID)
	if err != nil || after.Status != domain.RunStatusWaiting {
		t.Fatalf("run after drain: %+v %v", after, err)
	}
	tasks, err := st.ListTasksByRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Status != domain.TaskStatusReview {
			t.Fatalf("task status %s", task.Status)
		}
	}
}

func TestWorkerFailureWaitsForOtherResult(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "failure lead", Role: domain.Role{Name: domain.Teamlead}}
	front := domain.Agent{ID: uuid.New(), Name: "failure front", Model: "fail", BaseURL: "http://mock", Role: domain.Role{Name: domain.Frontend}}
	back := domain.Agent{ID: uuid.New(), Name: "failure back", Model: "ok", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &front, &back} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		started <- input.Model
		if input.Model == "fail" {
			return nil, fmt.Errorf("model failed")
		}
		<-release
		return streamResponse(`{"summary":"saved","done":true,"artifacts":[{"filename":"ok.txt","content":"saved"}]}`), nil
	})
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	find := func(role string) (domain.Agent, error) {
		if role == "frontend" {
			return front, nil
		}
		return back, nil
	}
	if err := eng.createTaskGraph(ctx, run.ID, []agent.TaskSpec{{Key: "a", Title: "fails", AssignTo: "frontend"}, {Key: "b", Title: "works", AssignTo: "backend"}}, find); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- eng.executeReadyTasks(ctx, run) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-done:
		close(release)
		t.Fatal("scheduler did not wait for other worker")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("expected error")
	}
	tasks, err := st.ListTasksByRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Text == "fails" && task.Status != domain.TaskStatusBlocked {
			t.Fatalf("failed task: %+v", task)
		}
		if task.Text == "works" && (task.Status != domain.TaskStatusReview || task.Summary != "saved") {
			t.Fatalf("successful task: %+v", task)
		}
	}
	artifacts, err := st.ListArtifactsByRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].Content != "saved" {
		t.Fatalf("artifacts: %+v %v", artifacts, err)
	}
}

func TestMoreThanFiveLeadDecisions(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "long lead", Model: "lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusPending}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls <= 5 {
			return streamResponse(`{"actions":[]}`), nil
		}
		return streamResponse(`{"actions":[{"type":"complete_run","summary":"done"}]}`), nil
	})
	eng := NewEngine(agent.NewClient(&http.Client{Transport: transport}), st, 40)
	if err := eng.Start(ctx, run.ID, run.Goal); err != nil {
		t.Fatal(err)
	}
	saved, err := st.GetRun(ctx, run.ID)
	if err != nil || saved.Status != domain.RunStatusFinished || saved.LeadSteps != 6 {
		t.Fatalf("run %+v: %v", saved, err)
	}
}

func TestRepeatedAcceptanceDoesNotStopReview(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "repeat lead", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", LeadID: lead.ID, Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	accepted := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "accepted", Status: domain.TaskStatusDone}
	review := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "review", Status: domain.TaskStatusReview}
	pending := domain.Task{ID: uuid.New(), RunID: run.ID, Text: "pending", Status: domain.TaskStatusTodo}
	for _, task := range []*domain.Task{&accepted, &review, &pending} {
		if err := st.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	eng := NewEngine(nil, st, 40)
	actions := []agent.Action{{Type: agent.ActionAcceptTask, TaskID: accepted.ID.String()}, {Type: agent.ActionAcceptTask, TaskID: review.ID.String()}}
	if err := eng.applyActions(ctx, run.ID, lead.ID, actions); err != nil {
		t.Fatal(err)
	}
	result, err := st.GetTask(ctx, review.ID)
	if err != nil || result.Status != domain.TaskStatusDone {
		t.Fatalf("review result: %+v, %v", result, err)
	}
	if err := eng.applyActions(ctx, run.ID, lead.ID, []agent.Action{{Type: agent.ActionAcceptTask, TaskID: pending.ID.String()}}); err == nil {
		t.Fatal("accepted a task that is not in review")
	}
	result, err = st.GetTask(ctx, pending.ID)
	if err != nil || result.Status != domain.TaskStatusTodo {
		t.Fatalf("pending result: %+v, %v", result, err)
	}
}
