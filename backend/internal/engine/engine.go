package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/rag"
	"github.com/yalama34/orchestrator/internal/store"
)

var errWaitingForUser = errors.New("[ENGINE] Waiting for user")

type Engine struct {
	Client      *agent.Client
	Store       *store.Store
	MaxSteps    int
	RAG         *rag.Service
	memorySlots chan struct{}
}

func NewEngine(client *agent.Client, store *store.Store, maxSteps int) *Engine {
	return &Engine{Client: client, Store: store, MaxSteps: maxSteps, memorySlots: make(chan struct{}, 2)}
}

func (e *Engine) decide(ctx context.Context, runID uuid.UUID, goal string) (uuid.UUID, []agent.Action, error) {
	run, err := e.Store.GetRun(ctx, runID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	leadAgent, err := e.Store.GetAgent(ctx, run.LeadID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	lead := agent.NewLead(leadAgent, e.Client)
	userPrompt, err := e.buildLeadUserPrompt(ctx, runID, goal)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("[ENGINE] Failed to build prompt: %w", err)
	}
	system := agent.LeadSystemPrompt()
	if lead.Agent.Role.SystemPrompt != "" {
		system = lead.Agent.Role.SystemPrompt
	}
	out, err := agent.Decide(ctx, lead, system, userPrompt)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("[ENGINE] Failed to decide: %w", err)
	}
	return leadAgent.ID, out.Actions, nil
}

func (e *Engine) Step(ctx context.Context, runID uuid.UUID, goal string) error {
	leadID, actions, err := e.decide(ctx, runID, goal)
	if err != nil {
		return err
	}
	if err := e.applyActions(ctx, runID, leadID, actions); err != nil {
		if errors.Is(err, errWaitingForUser) {
			return errWaitingForUser
		}
		return fmt.Errorf("[ENGINE] Failed to apply actions: %w", err)
	}
	run, err := e.Store.GetRun(ctx, runID)
	if err != nil || run.Status == domain.RunStatusFinished {
		return err
	}
	review := func() (uuid.UUID, []agent.Action, error) {
		allowed, err := e.Store.IncrementLeadSteps(ctx, runID, e.MaxSteps)
		if err != nil {
			return uuid.Nil, nil, err
		}
		if !allowed {
			return uuid.Nil, nil, fmt.Errorf("[ENGINE] Run reached maximum of %d lead decisions", e.MaxSteps)
		}
		return e.decide(ctx, runID, goal)
	}
	return e.executeReadyTasks(ctx, run, review)
}

func (e *Engine) executeTask(ctx context.Context, run domain.Run, task domain.Task) error {
	runID := run.ID
	if task.AssigneeID == nil {
		return fmt.Errorf("[ENGINE] Task %s has no assignee", task.ID)
	}
	workerAgent, err := e.Store.GetAgent(ctx, *task.AssigneeID)
	if err != nil {
		return err
	}
	var contextMD string
	if rc, err := e.Store.GetContext(ctx, runID); err == nil {
		contextMD = rc.Content
	}
	previousArtifacts, err := e.Store.ListArtifactsByTask(ctx, task.ID)
	if err != nil {
		return err
	}
	questions, err := e.Store.ListRunQuestions(ctx, runID)
	if err != nil {
		return err
	}

	worker := agent.NewWorker(workerAgent, e.Client, task.ID)
	worker.Dependencies, err = e.dependencyResults(ctx, runID, task.ID)
	if err != nil {
		return e.failTask(task.ID, "Не удалось загрузить результаты зависимых задач.", err)
	}
	worker.Memory = e.searchTaskMemory(ctx, runID, task)
	if run.SpecText != "" {
		worker.Sources, err = e.searchSpec(ctx, run, task)
		if err != nil {
			return e.failTask(task.ID, "Не удалось найти требования в ТЗ. Подробности доступны в логах сервера.", err)
		}
	}
	var lastUpdate time.Time
	progress := func(at time.Time) {
		if !lastUpdate.IsZero() && at.Sub(lastUpdate) < 3*time.Second {
			return
		}
		lastUpdate = at
		updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := e.Store.UpdateTaskModelActivity(updateCtx, task.ID, at); err != nil {
			log.Printf("[ENGINE] Could not save model activity for task %s: %v", task.ID, err)
		}
	}
	result, err := worker.Execute(ctx, task.Text, task.Details, task.Feedback, contextMD, previousArtifacts, questions, progress)
	if err != nil {
		reason := "Не удалось получить полный ответ модели. Подробности доступны в логах сервера."
		switch {
		case errors.Is(err, agent.ErrFirstContentTimeout):
			reason = "Модель не начала отвечать за 60 секунд."
		case errors.Is(err, agent.ErrStreamIdleTimeout):
			reason = "Модель не присылала данные 60 секунд."
		case errors.Is(err, agent.ErrStreamTotalTimeout):
			reason = "Превышен общий лимит запроса к модели — 10 минут."
		}
		return e.failTask(task.ID, reason, err)
	}
	var artifacts []domain.Artifact
	for _, art := range result.Artifacts {
		artifacts = append(artifacts, domain.Artifact{ID: uuid.New(), RunID: runID, TaskID: task.ID, Filename: art.Filename, Language: art.Language, Content: art.Content})
	}

	status := domain.TaskStatusBlocked
	if result.Done {
		status = domain.TaskStatusReview
	}
	if err := e.Store.SaveTaskOutput(ctx, task, artifacts, result.Summary, status); err != nil {
		if errors.Is(err, store.ErrArtifactConflict) {
			return e.failTask(task.ID, "Обнаружен конфликт имён файлов с другой независимой задачей.", err)
		}
		return e.failTask(task.ID, "Не удалось сохранить результат задачи. Подробности доступны в логах сервера.", err)
	}

	return nil
}

func (e *Engine) failTask(id uuid.UUID, reason string, err error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if saveErr := e.Store.FailTask(ctx, id, reason); saveErr != nil {
		return fmt.Errorf("%w; failed to record task failure: %v", err, saveErr)
	}
	return err
}

func (e *Engine) Start(ctx context.Context, runID uuid.UUID, goal string) (resultErr error) {
	defer func() {
		if resultErr != nil {
			reason := "Запуск остановился из-за ошибки. Подробности доступны в логах сервера."
			if errors.Is(resultErr, agent.ErrRequestRejected) {
				reason = "Провайдер модели отклонил запрос. Возможно, ТЗ и результаты задач превышают контекст модели; проверьте логи или сократите ТЗ."
			}
			if errors.Is(resultErr, errDependenciesBlocked) {
				reason = "Задачи ожидают заблокированные зависимости. Проверьте причины блокировки на доске."
			}
			if err := e.Store.FailRun(context.Background(), runID, reason); err != nil {
				resultErr = fmt.Errorf("%w; failed to update run status: %v", resultErr, err)
			}
		}
	}()
	run, err := e.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.SpecText) != "" && run.SpecStatus != "ready" {
		if err := e.Store.UpdateSpecStatus(ctx, runID, "processing", ""); err != nil {
			return err
		}
		indexCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := e.RAG.Index(indexCtx, run)
		cancel()
		if err != nil {
			if saveErr := e.Store.UpdateSpecStatus(context.Background(), runID, "failed", "Не удалось подготовить ТЗ. Проверьте LM Studio и настройки модели эмбеддингов."); saveErr != nil {
				return fmt.Errorf("%w; failed to save document status: %v", err, saveErr)
			}
			return fmt.Errorf("[ENGINE] Failed to index specification: %w", err)
		}
	}
	if err := e.Store.UpdateRunStatus(ctx, runID, domain.RunStatusRunning); err != nil {
		return err
	}
	for i := 0; i < e.MaxSteps; i++ {
		allowed, err := e.Store.IncrementLeadSteps(ctx, runID, e.MaxSteps)
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("[ENGINE] Run reached maximum of %d lead decisions", e.MaxSteps)
		}
		if err := e.Step(ctx, runID, goal); err != nil {
			if errors.Is(err, errWaitingForUser) {
				return nil
			}
			return fmt.Errorf("[ENGINE] Failed step %d: %w", i+1, err)
		}
		run, err := e.Store.GetRun(ctx, runID)
		if err != nil {
			return err
		}
		if run.Status == domain.RunStatusFinished {
			return nil
		}
	}
	return fmt.Errorf("[ENGINE] Run reached maximum of %d steps", e.MaxSteps)
}

func (e *Engine) buildLeadUserPrompt(ctx context.Context, runID uuid.UUID, goal string) (string, error) {
	run, err := e.Store.GetRun(ctx, runID)
	if err != nil {
		return "", err
	}
	tasks, err := e.Store.ListTasksByRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("[ENGINE] Failed to list tasks: %w", err)
	}

	var contextMD string
	if rc, err := e.Store.GetContext(ctx, runID); err == nil {
		contextMD = rc.Content
	}

	taskRows := make([]agent.LeadTaskRow, 0, len(tasks))
	var results []agent.LeadResultRow
	workers, err := e.Store.ListAgentsByRun(ctx, runID)
	if err != nil {
		return "", err
	}
	var roles []string
	dependencies, err := e.Store.ListTaskDependencies(ctx, runID)
	if err != nil {
		return "", err
	}
	taskNames := make(map[uuid.UUID]string, len(tasks))
	for _, task := range tasks {
		taskNames[task.ID] = task.Text
	}
	questions, err := e.Store.ListRunQuestions(ctx, runID)
	if err != nil {
		return "", err
	}
	for _, worker := range workers {
		roles = append(roles, string(worker.Role.Name))
	}
	for _, t := range tasks {
		var dependsOn []string
		for _, dependency := range dependencies {
			if dependency.TaskID == t.ID {
				dependsOn = append(dependsOn, taskNames[dependency.DependsOnID])
			}
		}
		assignee := "не назначен"
		if t.AssigneeID != nil {
			a, err := e.Store.GetAgent(ctx, *t.AssigneeID)
			if err == nil {
				assignee = string(a.Role.Name)
			}
		}
		taskRows = append(taskRows, agent.LeadTaskRow{
			ID:        t.ID.String(),
			Status:    string(t.Status),
			Assignee:  assignee,
			Text:      t.Text,
			Details:   t.Details,
			Feedback:  t.Feedback,
			DependsOn: dependsOn,
		})
		if t.Status == domain.TaskStatusReview || t.Status == domain.TaskStatusBlocked {
			artifacts, err := e.Store.ListArtifactsByTask(ctx, t.ID)
			if err != nil {
				return "", err
			}
			var sources []agent.KnowledgeSource
			if t.Status == domain.TaskStatusReview && run.SpecText != "" {
				sources, err = e.searchSpec(ctx, run, t)
				if err != nil {
					return "", err
				}
			}
			var memory []agent.MemorySource
			if t.Status == domain.TaskStatusReview {
				memory = e.searchTaskMemory(ctx, runID, t)
			}
			results = append(results, agent.LeadResultRow{TaskID: t.ID.String(), TaskText: t.Text, Summary: t.Summary, Done: t.Status == domain.TaskStatusReview, Artifacts: artifacts, Sources: sources, Memory: memory})
		}
	}

	data := agent.LeadUserPromptData{
		Goal:      goal,
		ContextMD: contextMD,
		Tasks:     taskRows,
		Results:   results,
		Roles:     roles,
		Questions: questions,
		SpecText:  run.SpecText,
		SpecName:  run.SpecName,
	}

	prompt, err := agent.LeadUserPrompt(data)
	if err != nil {
		return "", fmt.Errorf("[ENGINE] Failed to render prompt: %w", err)
	}
	return prompt, nil
}

func (e *Engine) searchSpec(ctx context.Context, run domain.Run, task domain.Task) ([]agent.KnowledgeSource, error) {
	searchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	chunks, err := e.RAG.Search(searchCtx, run, task.Text+"\n"+task.Details+"\n"+task.Feedback)
	if err != nil {
		_ = e.Store.UpdateSpecStatus(context.Background(), run.ID, "failed", "Не удалось выполнить поиск по ТЗ. Проверьте LM Studio и настройки модели эмбеддингов.")
		return nil, fmt.Errorf("[ENGINE] Failed to search specification: %w", err)
	}
	var sources []agent.KnowledgeSource
	for _, chunk := range chunks {
		sources = append(sources, agent.KnowledgeSource{Name: run.SpecName, Heading: chunk.Heading, Content: chunk.Content})
	}
	return sources, nil
}
