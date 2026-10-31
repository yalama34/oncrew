package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
)

type memoryPart struct {
	Source     string
	Filename   string
	ArtifactID *uuid.UUID
	Content    string
}

func splitMemoryText(text string) []string {
	const maxSize = 1800
	runes := []rune(text)
	var parts []string
	for len(runes) > 0 {
		end := min(maxSize, len(runes))
		if end < len(runes) {
			for i := end; i > end-400; i-- {
				if runes[i-1] == '\n' || runes[i-1] == ' ' || runes[i-1] == '\t' {
					end = i
					break
				}
			}
		}
		part := string(runes[:end])
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
		runes = runes[end:]
	}
	return parts
}

func (s *Service) IndexTask(ctx context.Context, task domain.Task) error {
	if s == nil || s.Client == nil || s.Store == nil || s.Client.Model == "" || s.Client.BaseURL == "" {
		return fmt.Errorf("[RAG] Embedding service is not configured")
	}
	artifacts, err := s.Store.ListArtifactsByTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("[RAG] Failed to load task artifacts: %w", err)
	}
	var filenames []string
	for _, artifact := range artifacts {
		filenames = append(filenames, artifact.Filename)
	}
	summary := fmt.Sprintf("Задача: %s\nОписание: %s\nРезультат: %s\nФайлы: %s", task.Text, task.Details, task.Summary, strings.Join(filenames, ", "))
	var parts []memoryPart
	for _, text := range splitMemoryText(summary) {
		parts = append(parts, memoryPart{Source: "task_summary", Content: text})
	}
	for _, artifact := range artifacts {
		artifactID := artifact.ID
		for _, text := range splitMemoryText(artifact.Content) {
			parts = append(parts, memoryPart{Source: "artifact", Filename: artifact.Filename, ArtifactID: &artifactID, Content: text})
		}
	}
	var chunks []store.MemoryChunk
	dimension := 0
	for start := 0; start < len(parts); start += 8 {
		end := min(start+8, len(parts))
		var texts []string
		for _, part := range parts[start:end] {
			title := task.Text
			if part.Filename != "" {
				title += " > " + part.Filename
			}
			text := title + "\n" + part.Content
			if strings.Contains(strings.ToLower(s.Client.Model), "embeddinggemma") {
				text = "title: " + title + " | text: " + part.Content
			}
			texts = append(texts, text)
		}
		embeddings, err := s.Client.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("[RAG] Failed to embed task memory: %w", err)
		}
		for i, embedding := range embeddings {
			if dimension == 0 {
				dimension = len(embedding)
			}
			if !validVector(embedding, dimension) {
				return fmt.Errorf("[RAG] Invalid task memory embedding")
			}
			part := parts[start+i]
			chunks = append(chunks, store.MemoryChunk{ID: uuid.New(), RunID: task.RunID, TaskID: task.ID, ArtifactID: part.ArtifactID, TaskTitle: task.Text, Source: part.Source, Filename: part.Filename, Content: part.Content, Model: s.Client.Model, Version: task.MemoryVersion, Embedding: store.Vector(embedding)})
		}
	}
	if err := s.Store.SaveTaskMemory(ctx, task.ID, task.MemoryVersion, chunks); err != nil {
		return fmt.Errorf("[RAG] Failed to save task memory: %w", err)
	}
	return nil
}

func (s *Service) SearchTaskMemory(ctx context.Context, runID uuid.UUID, query string, exclude []uuid.UUID) ([]store.MemoryChunk, error) {
	if s == nil || s.Client == nil || s.Store == nil || s.Client.Model == "" || s.Client.BaseURL == "" {
		return nil, nil
	}
	exists, err := s.Store.HasTaskMemory(ctx, runID, s.Client.Model, exclude)
	if err != nil {
		return nil, fmt.Errorf("[RAG] Failed to check task memory: %w", err)
	}
	if !exists {
		return nil, nil
	}
	if strings.Contains(strings.ToLower(s.Client.Model), "embeddinggemma") {
		query = "task: search result | query: " + query
	}
	vectors, err := s.Client.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("[RAG] Failed to embed task memory query: %w", err)
	}
	if len(vectors) != 1 || !validVector(vectors[0], len(vectors[0])) {
		return nil, fmt.Errorf("[RAG] Invalid task memory query embedding")
	}
	return s.Store.FindTaskMemory(ctx, runID, s.Client.Model, exclude, store.Vector(vectors[0]), 5)
}
