package rag

import (
	"net/http"
)

type Chunk struct {
	Heading string
	Content string
}

type EmbeddingClient struct {
	HTTP    *http.Client
	BaseURL string
	Model   string
	APIKey  string
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}
