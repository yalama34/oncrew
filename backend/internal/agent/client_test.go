package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yalama34/orchestrator/internal/domain"
)

func testStreamClient(t *testing.T, handler http.HandlerFunc) (*Client, domain.Agent) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.Client()), domain.Agent{BaseURL: server.URL, Model: "test"}
}
func sendEvent(w http.ResponseWriter, event string) {
	fmt.Fprintf(w, "data: %s\n\n", event)
	w.(http.Flusher).Flush()
}
func TestChatStreamAssemblesContent(t *testing.T) {
	var progress atomic.Int32
	c, a := testStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !request.Stream {
			t.Errorf("stream not requested: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keepalive\n\n")
		w.(http.Flusher).Flush()
		sendEvent(w, `{"choices":[{"delta":{"role":"assistant"}}]}`)
		sendEvent(w, `{"choices":[{"delta":{"reasoning":"thinking"}}]}`)
		sendEvent(w, `{"choices":[{"delta":{"content":"{\"ok\":"}}]}`)
		sendEvent(w, `{"choices":[{"delta":{"content":"true}"},"finish_reason":"stop"}]}`)
		sendEvent(w, "[DONE]")
	})
	out, err := c.Chat(context.Background(), a, "system", "user", func(time.Time) { progress.Add(1) })
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("output %q: %v", out, err)
	}
	if progress.Load() != 3 {
		t.Fatalf("got %d meaningful events", progress.Load())
	}
}
func TestChatRejectsIncompleteOrBrokenStreams(t *testing.T) {
	tests := []struct{ name, stream string }{
		{"provider error", "data: {\"error\":{\"message\":\"failed\"}}\n\n"},
		{"invalid json", "data: {broken}\n\n"},
		{"missing done", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":\"stop\"}]}\n\n"},
		{"truncated response", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"},
		{"missing finish", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"},
		{"empty", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, a := testStreamClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tt.stream) })
			if out, err := c.Chat(context.Background(), a, "", ""); err == nil || out != "" {
				t.Fatalf("expected failure, got %q %v", out, err)
			}
		})
	}
}
func TestChatStreamTimeouts(t *testing.T) {
	tests := []struct {
		name               string
		first, idle, total time.Duration
		content            bool
		want               error
	}{
		{"first ignores keepalives", 70 * time.Millisecond, time.Second, time.Second, false, ErrFirstContentTimeout},
		{"idle", time.Second, 70 * time.Millisecond, time.Second, true, ErrStreamIdleTimeout},
		{"total", time.Second, time.Second, 70 * time.Millisecond, true, ErrStreamTotalTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, a := testStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.content {
					sendEvent(w, `{"choices":[{"delta":{"content":"x"}}]}`)
				} else {
					fmt.Fprint(w, ": keepalive\n\n")
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			})
			c.firstTimeout, c.idleTimeout, c.totalTimeout = tt.first, tt.idle, tt.total
			_, err := c.Chat(context.Background(), a, "", "")
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v want %v", err, tt.want)
			}
		})
	}
}
func TestChatHTTPError(t *testing.T) {
	c, a := testStreamClient(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret body", 429) })
	_, err := c.Chat(context.Background(), a, "", "")
	if err == nil || !strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "secret body") {
		t.Fatal(err)
	}
}
