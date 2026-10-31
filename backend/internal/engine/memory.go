package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/agent"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
)

func (e *Engine) queueTaskMemory(task domain.Task) {
	go func() {
		e.memorySlots <- struct{}{}
		defer func() { <-e.memorySlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := e.RAG.IndexTask(ctx, task); err != nil {
			if errors.Is(err, store.ErrMemoryStale) {
				return
			}
			log.Printf("[ENGINE] Failed to index task memory %s: %v", task.ID, err)
			saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer saveCancel()
			if saveErr := e.Store.FailTaskMemory(saveCtx, task.ID, task.MemoryVersion, "Не удалось индексировать результат. Проверьте сервер и модель эмбеддингов."); saveErr != nil {
				log.Printf("[ENGINE] Failed to save memory error for %s: %v", task.ID, saveErr)
			}
		}
	}()
}

func (e *Engine) ResumePendingTaskMemory(ctx context.Context) error {
	tasks, err := e.Store.PendingTaskMemories(ctx)
	if err != nil {
		return fmt.Errorf("[ENGINE] Failed to load pending task memory: %w", err)
	}
	for _, task := range tasks {
		e.queueTaskMemory(task)
	}
	return nil
}

func (e *Engine) RetryTaskMemory(ctx context.Context, taskID uuid.UUID) error {
	task, err := e.Store.RetryTaskMemory(ctx, taskID)
	if err != nil {
		return err
	}
	e.queueTaskMemory(task)
	return nil
}

func (e *Engine) searchTaskMemory(ctx context.Context, runID uuid.UUID, task domain.Task) []agent.MemorySource {
	if e.RAG == nil {
		return nil
	}
	dependencies, err := e.Store.ListTaskDependencies(ctx, runID)
	if err != nil {
		log.Printf("[ENGINE] Failed to load dependencies for task memory: %v", err)
		return nil
	}
	exclude := []uuid.UUID{task.ID}
	for _, dependency := range dependencies {
		if dependency.TaskID == task.ID {
			exclude = append(exclude, dependency.DependsOnID)
		}
	}
	searchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	chunks, err := e.RAG.SearchTaskMemory(searchCtx, runID, task.Text+"\n"+task.Details+"\n"+task.Feedback, exclude)
	if err != nil {
		log.Printf("[ENGINE] Failed to search task memory for %s: %v", task.ID, err)
		return nil
	}
	var sources []agent.MemorySource
	for _, chunk := range chunks {
		source := "результат задачи"
		if chunk.Source == "artifact" {
			source = "артефакт"
		}
		sources = append(sources, agent.MemorySource{TaskID: chunk.TaskID.String(), TaskTitle: chunk.TaskTitle, Source: source, Filename: chunk.Filename, Content: chunk.Content})
	}
	return sources
}
