package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestEmbed(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "test-model" || len(req.Input) != 2 {
			t.Errorf("unexpected body: %+v, error: %v", req, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"index":1,"embedding":[2]},{"index":0,"embedding":[1]}]}`)),
		}, nil
	})}

	client := NewEmbeddingClient(httpClient)
	client.BaseURL = "http://localhost:1234"
	client.Model = "test-model"
	result, err := client.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0][0] != 1 || result[1][0] != 2 {
		t.Fatalf("unexpected embeddings: %v", result)
	}
}
