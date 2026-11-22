package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
)

func TestFilePlanSerializesAndUsesLatestVersion(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "plan lead", Role: domain.Role{Name: domain.Teamlead}}
	worker := domain.Agent{ID: uuid.New(), Name: "plan worker", Role: domain.Role{Name: domain.Frontend}}
	for _, a := range []*domain.Agent{&lead, &worker} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.Run{ID: uuid.New(), LeadID: lead.ID, Goal: "file plan", Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	eng := NewEngine(nil, st, 40)
	specs := []agent.TaskSpec{
		{Key: "a", Title: "first", AssignTo: "frontend", Reads: []string{}, Writes: []string{"file.js"}},
		{Key: "b", Title: "second", AssignTo: "frontend", Reads: []string{"file.js"}, Writes: []string{"file.js"}},
	}
	if err := eng.createTaskGraph(ctx, run.ID, specs, func(string) (domain.Agent, error) { return worker, nil }); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasksByRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]domain.Task{}
	for _, task := range tasks {
		byName[task.Text] = task
	}
	deps, err := st.ListTaskDependencies(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].TaskID != byName["second"].ID || deps[0].DependsOnID != byName["first"].ID {
		t.Fatalf("unexpected dependencies: %+v", deps)
	}
	first := byName["first"]
	if ok, err := st.ClaimTask(ctx, first.ID); err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	if _, err := st.SnapshotTaskFiles(ctx, first); err != nil {
		t.Fatal(err)
	}
	a := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: first.ID, Filename: "file.js", Content: "one"}
	if err := st.SaveTaskOutput(ctx, first, []domain.Artifact{a}, "first", domain.TaskStatusReview); err != nil {
		t.Fatal(err)
	}
	second := byName["second"]
	if ok, err := st.ClaimTask(ctx, second.ID); err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	input, err := st.SnapshotTaskFiles(ctx, second)
	if err != nil || len(input) != 1 || input[0].Content != "one" {
		t.Fatalf("snapshot: %+v, %v", input, err)
	}
	bad := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: second.ID, Filename: "other.js", Content: "bad"}
	if err := st.SaveTaskOutput(ctx, second, []domain.Artifact{bad}, "", domain.TaskStatusReview); !errors.Is(err, store.ErrUnplannedArtifact) {
		t.Fatalf("expected unplanned error, got %v", err)
	}
	b := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: second.ID, Filename: "file.js", Content: "two"}
	if err := st.SaveTaskOutput(ctx, second, []domain.Artifact{b}, "second", domain.TaskStatusReview); err != nil {
		t.Fatal(err)
	}
	versions, err := st.ListArtifactVersions(ctx, run.ID, a.ID)
	if err != nil || len(versions) != 2 || versions[1].Content != "two" {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	third := domain.Task{ID: uuid.New(), RunID: run.ID, Status: domain.TaskStatusTodo, Text: "third", FilePlan: true, AssigneeID: &worker.ID}
	fourth := domain.Task{ID: uuid.New(), RunID: run.ID, Status: domain.TaskStatusTodo, Text: "fourth", FilePlan: true, AssigneeID: &worker.ID}
	files := []domain.TaskFile{{TaskID: third.ID, Path: "file.js", Read: true, Write: true}, {TaskID: fourth.ID, Path: "file.js", Read: true, Write: true}}
	if err := st.CreateTaskGraph(ctx, []domain.Task{third, fourth}, nil, nil, files); err != nil {
		t.Fatal(err)
	}
	for _, task := range []domain.Task{third, fourth} {
		if ok, err := st.ClaimTask(ctx, task.ID); err != nil || !ok {
			t.Fatalf("claim: %v", err)
		}
		if _, err := st.SnapshotTaskFiles(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	newArtifact := domain.Artifact{ID: uuid.New(), RunID: run.ID, TaskID: third.ID, Filename: "file.js", Content: "three"}
	if err := st.SaveTaskOutput(ctx, third, []domain.Artifact{newArtifact}, "", domain.TaskStatusReview); err != nil {
		t.Fatal(err)
	}
	newArtifact.ID = uuid.New()
	newArtifact.TaskID = fourth.ID
	newArtifact.Content = "stale"
	if err := st.SaveTaskOutput(ctx, fourth, []domain.Artifact{newArtifact}, "", domain.TaskStatusReview); !errors.Is(err, store.ErrArtifactConflict) {
		t.Fatalf("expected stale version conflict, got %v", err)
	}
}

func TestLeadFilePlanValidation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	eng := NewEngine(nil, st, 40)
	runID := uuid.New()
	missing := []agent.Action{{Type: agent.ActionCreateTasks, Tasks: []agent.TaskSpec{{Key: "a", Title: "missing plan"}}}}
	if err := eng.validateLeadFilePlans(ctx, runID, missing); err == nil {
		t.Fatal("missing arrays accepted")
	}
	missing[0].Tasks[0].Reads = []string{"absent.js"}
	missing[0].Tasks[0].Writes = []string{}
	if err := eng.validateLeadFilePlans(ctx, runID, missing); err == nil {
		t.Fatal("missing file accepted")
	}
	ordered := []agent.Action{{Type: agent.ActionCreateTasks, Tasks: []agent.TaskSpec{
		{Key: "a", Title: "create", Reads: []string{}, Writes: []string{"new.js"}},
		{Key: "b", Title: "use", DependsOn: []string{"a"}, Reads: []string{"new.js"}, Writes: []string{}},
	}}}
	if err := eng.validateLeadFilePlans(ctx, runID, ordered); err != nil {
		t.Fatal(err)
	}
}

func TestLeadRetriesInvalidFilePlan(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lead := domain.Agent{ID: uuid.New(), Name: "plan retry lead", Model: "mock", BaseURL: "http://mock", Role: domain.Role{Name: domain.Teamlead}}
	if err := st.CreateAgent(ctx, &lead); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: uuid.New(), LeadID: lead.ID, Goal: "retry plan", Status: domain.RunStatusRunning}
	if err := st.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := agent.NewClient(&http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return streamResponse(`{"actions":[{"type":"create_tasks","tasks":[{"title":"task"}]}]}`), nil
		}
		return streamResponse(`{"actions":[{"type":"create_tasks","tasks":[{"title":"task","reads":[],"writes":[]}]}]}`), nil
	})})
	eng := NewEngine(client, st, 40)
	_, actions, err := eng.decide(ctx, run.ID, run.Goal)
	if err != nil || calls != 2 || len(actions) != 1 {
		t.Fatalf("calls=%d actions=%v error=%v", calls, actions, err)
	}
	updated, err := st.GetRun(ctx, run.ID)
	if err != nil || updated.LeadSteps != 1 {
		t.Fatalf("lead steps=%d error=%v", updated.LeadSteps, err)
	}
	if !strings.Contains(actions[0].Tasks[0].Title, "task") {
		t.Fatal("unexpected corrected action")
	}
}
