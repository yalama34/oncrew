package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func NewEmbeddingClient(h *http.Client) *EmbeddingClient {
	if h == nil {
		return nil
	}
	return &EmbeddingClient{
		HTTP: h,
	}
}

func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if c == nil || c.HTTP == nil || c.BaseURL == "" || c.Model == "" {
		return nil, fmt.Errorf("[EMBEDDING] HTTP client, base URL and model are required")
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("[EMBEDDING] No texts to embed")
	}

	body, err := json.Marshal(embeddingRequest{Model: c.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("[EMBEDDING] Failed to marshal request: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("[EMBEDDING] Failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[EMBEDDING] Failed to get response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[EMBEDDING] API returned status %s", resp.Status)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[EMBEDDING] Failed to read response: %w", err)
	}
	var response embeddingResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("[EMBEDDING] Failed to unmarshal response: %w", err)
	}
	if len(response.Data) != len(texts) {
		return nil, fmt.Errorf("[EMBEDDING] Expected %d embeddings, got %d", len(texts), len(response.Data))
	}

	embeddings := make([][]float64, len(texts))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(texts) || embeddings[item.Index] != nil || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("[EMBEDDING] Invalid embedding at index %d", item.Index)
		}
		embeddings[item.Index] = item.Embedding
	}
	return embeddings, nil
}
