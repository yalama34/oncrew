package agent

import (
	"context"
	"encoding/json"
	"github.com/yalama34/orchestrator/internal/domain"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveOpenRouterStream(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_LIVE_STREAM_TEST") != "1" {
		t.Skip("set ORCHESTRATOR_LIVE_STREAM_TEST=1 to use OpenRouter")
	}
	key := os.Getenv("LLM_API_KEY")
	if key == "" {
		t.Fatal("LLM_API_KEY is required")
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		t.Fatal("LLM_MODEL is required")
	}
	c := NewClient(&http.Client{})
	count := 0
	out, err := c.Chat(context.Background(), domain.Agent{BaseURL: "https://openrouter.ai/api", APIKey: key, Model: model}, "Return only a JSON object.", "Return the JSON object {\"ok\": true}.", func(_ time.Time) { count++ })
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &v); err != nil || v["ok"] != true {
		t.Fatalf("unexpected model output: %q, %v", out, err)
	}
	if count == 0 {
		t.Fatal("stream did not deliver content events")
	}
	t.Logf("Received %d meaningful stream events", count)
}
