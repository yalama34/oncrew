package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/engine"
	"github.com/yalama34/orchestrator/internal/store"
	"github.com/yalama34/orchestrator/internal/store/postgres"
	pg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func flowStreamResponse(output string) *http.Response {
	data, _ := json.Marshal(map[string]interface{}{"choices": []interface{}{map[string]interface{}{"delta": map[string]string{"content": output}, "finish_reason": "stop"}}})
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + string(data) + "\n\ndata: [DONE]\n\n")), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
}

type flowTransport struct {
	mu          sync.Mutex
	leadCalls   int
	workerCalls int
	problem     string
}

type flowRoundTrip func(*http.Request) (*http.Response, error)

func (f flowRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (f *flowTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil || len(request.Messages) != 2 {
		return nil, fmt.Errorf("invalid model request: %v", err)
	}
	system, prompt := request.Messages[0].Content, request.Messages[1].Content
	var output string
	switch request.Model {
	case "selected-lead":
		f.leadCalls++
		switch f.leadCalls {
		case 1:
			output = `{"actions":[{"type":"create_tasks","tasks":[{"title":"Сделать API","details":"Использовать JSON","assign_to":"backend"}]}]}`
		case 2:
			if !strings.Contains(prompt, "первый результат") || !strings.Contains(prompt, "version one") {
				f.problem = "lead did not receive worker summary and artifact"
			}
			id := regexp.MustCompile(`id=([0-9a-f-]{36})`).FindStringSubmatch(prompt)
			if len(id) < 2 {
				return nil, fmt.Errorf("missing task id in lead prompt")
			}
			output = fmt.Sprintf(`{"actions":[{"type":"rework_task","task_id":%q,"feedback":"Исправить API"}]}`, id[1])
		case 3:
			if !strings.Contains(prompt, "исправленный результат") || !strings.Contains(prompt, "version two") {
				f.problem = "lead did not receive updated result"
			}
			id := regexp.MustCompile(`id=([0-9a-f-]{36})`).FindStringSubmatch(prompt)
			if len(id) < 2 {
				return nil, fmt.Errorf("missing task id in lead prompt")
			}
			output = fmt.Sprintf(`{"actions":[{"type":"accept_task","task_id":%q},{"type":"complete_run","summary":"готово"}]}`, id[1])
		default:
			return nil, fmt.Errorf("unexpected lead call")
		}
	case "selected-worker":
		f.workerCalls++
		if !strings.Contains(system, "backend") || !strings.Contains(prompt, "Использовать JSON") {
			f.problem = "worker missing role prompt or task details"
		}
		if f.workerCalls == 1 {
			output = `{"summary":"первый результат","done":true,"artifacts":[{"filename":"api.txt","language":"text","content":"version one"}]}`
		} else if f.workerCalls == 2 {
			if !strings.Contains(prompt, "Исправить API") || !strings.Contains(prompt, "version one") {
				f.problem = "worker missing feedback or previous artifact"
			}
			output = `{"summary":"исправленный результат","done":true,"artifacts":[{"filename":"api.txt","language":"text","content":"version two"}]}`
		} else {
			return nil, fmt.Errorf("unexpected worker call")
		}
	default:
		return nil, fmt.Errorf("unexpected model: %s", request.Model)
	}
	return flowStreamResponse(output), nil
}

func TestFlowWithPostgres(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN to run the PostgreSQL flow test")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "chosen lead", Model: "selected-lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "chosen worker", Model: "selected-worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	outsider := domain.Agent{ID: uuid.New(), Name: "outside", Model: "wrong-worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &worker, &outsider} {
		if err := st.CreateAgent(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	mock := &flowTransport{}
	eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: mock}), &st, 5)
	router := NewRouter(&AgentHandler{Store: &st}, &RunHandler{Store: &st, Engine: eng}, &TaskHandler{Store: &st}, &ArtifactHandler{Store: &st})
	body, _ := json.Marshal(RunRequest{Goal: "Сделать API", LeadID: lead.ID.String(), AgentIDs: []string{worker.ID.String()}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var run domain.Run
	for time.Now().Before(deadline) {
		run, err = st.GetRun(context.Background(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == domain.RunStatusFinished || run.Status == domain.RunStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != domain.RunStatusFinished {
		t.Fatalf("run status: %s", run.Status)
	}
	tasks, err := st.ListTasksByRun(context.Background(), created.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %+v, %v", tasks, err)
	}
	if tasks[0].Status != domain.TaskStatusDone || tasks[0].Summary != "исправленный результат" || tasks[0].Details != "Использовать JSON" || tasks[0].AssigneeID == nil || *tasks[0].AssigneeID != worker.ID {
		t.Fatalf("unexpected task: %+v", tasks[0])
	}
	runResponse := httptest.NewRecorder()
	router.ServeHTTP(runResponse, httptest.NewRequest(http.MethodGet, "/api/runs/"+created.ID.String(), nil))
	var runDTO RunDTO
	if err := json.Unmarshal(runResponse.Body.Bytes(), &runDTO); err != nil || runResponse.Code != http.StatusOK || runDTO.TasksTotal != 1 || runDTO.TasksDone != 1 {
		t.Fatalf("unexpected run response: %s, error: %v", runResponse.Body.String(), err)
	}
	if _, err := time.Parse(time.RFC3339Nano, runDTO.CreatedAt); err != nil {
		t.Fatalf("invalid timestamp %q: %v", runDTO.CreatedAt, err)
	}
	taskResponse := httptest.NewRecorder()
	router.ServeHTTP(taskResponse, httptest.NewRequest(http.MethodGet, "/api/runs/"+created.ID.String()+"/tasks", nil))
	var taskDTOs []TaskDTO
	if err := json.Unmarshal(taskResponse.Body.Bytes(), &taskDTOs); err != nil || taskResponse.Code != http.StatusOK || len(taskDTOs) != 1 {
		t.Fatalf("unexpected task response: %s, error: %v", taskResponse.Body.String(), err)
	}
	if taskDTOs[0].Details != "Использовать JSON" || taskDTOs[0].Summary != "исправленный результат" || taskDTOs[0].AssigneeID == nil || *taskDTOs[0].AssigneeID != worker.ID.String() {
		t.Fatalf("unexpected task DTO: %+v", taskDTOs[0])
	}
	artifacts, err := st.ListArtifactsByTask(context.Background(), tasks[0].ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].Content != "version two" {
		t.Fatalf("artifacts: %+v, %v", artifacts, err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.problem != "" || mock.leadCalls != 3 || mock.workerCalls != 2 {
		t.Fatalf("model calls: lead=%d worker=%d problem=%s", mock.leadCalls, mock.workerCalls, mock.problem)
	}
}

func TestRunFailuresWithPostgres(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN to run the PostgreSQL flow test")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "failure lead", Model: "lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(context.Background(), &lead); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		transport flowRoundTrip
	}{
		{"model error", func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("model unavailable") }},
		{"step limit", func(*http.Request) (*http.Response, error) {
			return flowStreamResponse(`{"actions":[]}`), nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := domain.Run{ID: uuid.New(), Goal: "test", Status: domain.RunStatusPending, LeadID: lead.ID}
			if err := st.CreateRun(context.Background(), &run); err != nil {
				t.Fatal(err)
			}
			eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: tc.transport}), &st, 1)
			if err := eng.Start(context.Background(), run.ID, run.Goal); err == nil {
				t.Fatal("expected error")
			}
			got, err := st.GetRun(context.Background(), run.ID)
			if err != nil || got.Status != domain.RunStatusFailed {
				t.Fatalf("status=%s error=%v", got.Status, err)
			}
		})
	}
}

func TestBlockedResultWithPostgres(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN to run the PostgreSQL flow test")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "lead", Model: "lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "worker", Model: "worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", Status: domain.RunStatusPending, LeadID: lead.ID, Agents: []domain.Agent{worker}}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	leadCalls := 0
	var leadSawReason bool
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
		var result string
		if input.Model == "worker" {
			result = `{"summary":"нет схемы API","done":false,"artifacts":[]}`
		} else {
			leadCalls++
			if leadCalls == 1 {
				result = `{"actions":[{"type":"create_tasks","tasks":[{"title":"API","assign_to":"backend"}]}]}`
			} else {
				leadSawReason = strings.Contains(input.Messages[1].Content, "нет схемы API") && strings.Contains(input.Messages[1].Content, "done=false")
				result = `{"actions":[]}`
			}
		}
		return flowStreamResponse(result), nil
	})
	eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: transport}), &st, 2)
	if err := eng.Step(context.Background(), run.ID, run.Goal); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasksByRun(context.Background(), run.ID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != domain.TaskStatusBlocked || tasks[0].Summary != "нет схемы API" {
		t.Fatalf("tasks=%+v error=%v", tasks, err)
	}
	if err := eng.Step(context.Background(), run.ID, run.Goal); err != nil {
		t.Fatal(err)
	}
	if !leadSawReason {
		t.Fatal("lead did not receive blocked result")
	}
}

func TestAskUserAndResumeWithPostgres(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN to run the PostgreSQL flow test")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "question lead", Model: "question-lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "question worker", Model: "question-worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), Goal: "Проверить уточнение", Status: domain.RunStatusPending, LeadID: lead.ID, Agents: []domain.Agent{worker}}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	leadCalls, workerCalls := 0, 0
	var problem string
	transport := flowRoundTrip(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil || len(input.Messages) != 2 {
			return nil, fmt.Errorf("invalid request")
		}
		var result string
		if input.Model == "question-worker" {
			workerCalls++
			if !strings.Contains(input.Messages[1].Content, "Ответ: тестовые данные") {
				problem = "worker did not receive answer"
			}
			result = `{"summary":"готово","done":true,"artifacts":[]}`
		} else {
			leadCalls++
			switch leadCalls {
			case 1:
				result = `{"actions":[{"type":"create_tasks","tasks":[{"title":"Анализ","assign_to":"backend"}]},{"type":"ask_user","text":"Какие данные использовать?"},{"type":"complete_run","summary":"не должно выполниться"}]}`
			case 2:
				if !strings.Contains(input.Messages[1].Content, "Ответ: тестовые данные") {
					problem = "lead did not receive answer"
				}
				result = `{"actions":[]}`
			case 3:
				id := regexp.MustCompile(`id=([0-9a-f-]{36})`).FindStringSubmatch(input.Messages[1].Content)
				if len(id) < 2 {
					return nil, fmt.Errorf("missing task id")
				}
				result = fmt.Sprintf(`{"actions":[{"type":"accept_task","task_id":%q},{"type":"complete_run","summary":"готово"}]}`, id[1])
			default:
				return nil, fmt.Errorf("unexpected lead call")
			}
		}
		return flowStreamResponse(result), nil
	})
	eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: transport}), &st, 5)
	if err := eng.Start(context.Background(), run.ID, run.Goal); err != nil {
		t.Fatal(err)
	}
	waiting, err := st.GetRun(context.Background(), run.ID)
	if err != nil || waiting.Status != domain.RunStatusWaiting {
		t.Fatalf("waiting run: %+v %v", waiting, err)
	}
	tasks, err := st.ListTasksByRun(context.Background(), run.ID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != domain.TaskStatusTodo {
		t.Fatalf("tasks before answer: %+v %v", tasks, err)
	}
	mu.Lock()
	beforeAnswerCalls := workerCalls
	mu.Unlock()
	if beforeAnswerCalls != 0 {
		t.Fatal("worker started before answer")
	}
	router := NewRouter(&AgentHandler{Store: &st}, &RunHandler{Store: &st, Engine: eng}, &TaskHandler{Store: &st}, &ArtifactHandler{Store: &st})
	questionsResponse := httptest.NewRecorder()
	router.ServeHTTP(questionsResponse, httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID.String()+"/questions", nil))
	var questions []QuestionDTO
	if err := json.Unmarshal(questionsResponse.Body.Bytes(), &questions); err != nil || len(questions) != 1 || questions[0].Answer != nil || questions[0].Question != "Какие данные использовать?" {
		t.Fatalf("questions: %s, %v", questionsResponse.Body.String(), err)
	}
	emptyBody, _ := json.Marshal(AnswerRequest{QuestionID: questions[0].ID, Answer: "  "})
	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID.String()+"/answers", bytes.NewReader(emptyBody)))
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("empty answer status: %d", emptyResponse.Code)
	}
	wrongBody, _ := json.Marshal(AnswerRequest{QuestionID: uuid.NewString(), Answer: "ответ"})
	wrongResponse := httptest.NewRecorder()
	router.ServeHTTP(wrongResponse, httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID.String()+"/answers", bytes.NewReader(wrongBody)))
	if wrongResponse.Code != http.StatusConflict {
		t.Fatalf("wrong question status: %d", wrongResponse.Code)
	}
	body, _ := json.Marshal(AnswerRequest{QuestionID: questions[0].ID, Answer: "тестовые данные"})
	answerResponse := httptest.NewRecorder()
	router.ServeHTTP(answerResponse, httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID.String()+"/answers", bytes.NewReader(body)))
	if answerResponse.Code != http.StatusAccepted {
		t.Fatalf("answer: %d %s", answerResponse.Code, answerResponse.Body.String())
	}
	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID.String()+"/answers", bytes.NewReader(body)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate answer status: %d", duplicate.Code)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		waiting, err = st.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if waiting.Status == domain.RunStatusFinished || waiting.Status == domain.RunStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if waiting.Status != domain.RunStatusFinished {
		t.Fatalf("resumed status: %s", waiting.Status)
	}
	saved, err := st.ListRunQuestions(context.Background(), run.ID)
	if err != nil || len(saved) != 1 || saved[0].Answer == nil || *saved[0].Answer != "тестовые данные" {
		t.Fatalf("saved question: %+v %v", saved, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if problem != "" || leadCalls != 3 || workerCalls != 1 {
		t.Fatalf("lead=%d worker=%d problem=%s", leadCalls, workerCalls, problem)
	}
}

func TestConcurrentAnswersWithPostgres(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORCHESTRATOR_TEST_DSN to run the PostgreSQL flow test")
	}
	db, err := gorm.Open(pg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.CreateTables(db); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(db)
	lead := domain.Agent{ID: uuid.New(), Name: "concurrent lead", Model: "lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(context.Background(), &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), Goal: "test", Status: domain.RunStatusRunning, LeadID: lead.ID}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRunQuestion(context.Background(), run.ID, "Вопрос?"); err != nil {
		t.Fatal(err)
	}
	questions, err := st.ListRunQuestions(context.Background(), run.ID)
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions: %+v %v", questions, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := st.AnswerRunQuestion(context.Background(), run.ID, questions[0].ID, "ответ")
			results <- err
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, store.ErrRunNotWaiting) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestWorkerStreamFailureIsPersisted(t *testing.T) {
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
	lead := domain.Agent{ID: uuid.New(), Name: "stream lead", Model: "stream-lead", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "stream worker", Model: "stream-worker", BaseURL: "http://mock", Role: domain.Role{Name: domain.Backend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), LeadID: lead.ID, Goal: "stream failure", Status: domain.RunStatusPending}
	if err := st.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRunAgents(context.Background(), run.ID, []domain.Agent{worker}); err != nil {
		t.Fatal(err)
	}
	transport := flowRoundTrip(func(req *http.Request) (*http.Response, error) {
		var r struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(req.Body).Decode(&r); err != nil {
			return nil, err
		}
		if r.Model == "stream-lead" {
			return flowStreamResponse(`{"actions":[{"type":"create_tasks","tasks":[{"title":"Test stream","details":"test","assign_to":"backend"}]}]}`), nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")), Header: make(http.Header)}, nil
	})
	eng := engine.NewEngine(agent.NewClient(&http.Client{Transport: transport}), &st, 5)
	if err := eng.Start(context.Background(), run.ID, run.Goal); err == nil {
		t.Fatal("expected incomplete stream failure")
	}
	saved, err := st.GetRun(context.Background(), run.ID)
	if err != nil || saved.Status != domain.RunStatusFailed {
		t.Fatalf("run: %+v %v", saved, err)
	}
	tasks, err := st.ListTasksByRun(context.Background(), run.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	if tasks[0].Status != domain.TaskStatusBlocked || tasks[0].FailureReason == "" || tasks[0].ModelPhase != "" || tasks[0].ModelLastActivityAt == nil {
		t.Fatalf("task failure not saved: %+v", tasks[0])
	}
}
