package rag

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
)

type Service struct {
	Client *EmbeddingClient
	Store  *store.Store
}

func (s *Service) Index(ctx context.Context, run domain.Run) error {
	if s == nil || s.Client == nil || s.Store == nil || s.Client.Model == "" || s.Client.BaseURL == "" {
		return fmt.Errorf("[RAG] Embedding service is not configured")
	}
	parts := SplitMarkdown(run.SpecText)
	if len(parts) == 0 {
		return fmt.Errorf("[RAG] Document has no searchable text")
	}
	documentID := uuid.New()
	chunks := make([]store.DocumentChunk, 0, len(parts))
	dimension := 0
	for start := 0; start < len(parts); start += 8 {
		end := min(start+8, len(parts))
		var texts []string
		for _, part := range parts[start:end] {
			title := run.SpecName
			if part.Heading != "" {
				title += " > " + part.Heading
			}
			text := title + "\n" + part.Content
			if strings.Contains(strings.ToLower(s.Client.Model), "embeddinggemma") {
				text = "title: " + title + " | text: " + part.Content
			}
			texts = append(texts, text)
		}
		embeddings, err := s.Client.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("[RAG] Failed to embed document: %w", err)
		}
		for i, embedding := range embeddings {
			if dimension == 0 {
				dimension = len(embedding)
			}
			if !validVector(embedding, dimension) {
				return fmt.Errorf("[RAG] Invalid document embedding")
			}
			part := parts[start+i]
			chunks = append(chunks, store.DocumentChunk{ID: uuid.New(), RunID: run.ID, DocumentID: documentID, Heading: part.Heading, Content: part.Content, Model: s.Client.Model, Embedding: store.Vector(embedding)})
		}
	}
	if err := s.Store.SaveDocumentChunks(ctx, run.ID, chunks); err != nil {
		return fmt.Errorf("[RAG] Failed to save document: %w", err)
	}
	return nil
}

func (s *Service) Search(ctx context.Context, run domain.Run, query string) ([]store.DocumentChunk, error) {
	if s == nil || s.Client == nil || s.Store == nil {
		return nil, fmt.Errorf("[RAG] Embedding service is not configured")
	}
	if strings.Contains(strings.ToLower(s.Client.Model), "embeddinggemma") {
		query = "task: search result | query: " + query
	}
	embeddings, err := s.Client.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("[RAG] Failed to embed query: %w", err)
	}
	if len(embeddings) != 1 || !validVector(embeddings[0], len(embeddings[0])) {
		return nil, fmt.Errorf("[RAG] Invalid query embedding")
	}
	chunks, err := s.Store.FindDocumentChunks(ctx, run.ID, s.Client.Model, store.Vector(embeddings[0]), 5)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("[RAG] No indexed chunks for this model or vector dimension")
	}
	return chunks, nil
}

func validVector(vector []float64, dimension int) bool {
	if len(vector) == 0 || len(vector) != dimension {
		return false
	}
	var sum float64
	for _, v := range vector {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		sum += v * v
	}
	return sum > 0 && !math.IsInf(sum, 0)
}
