package preview

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPreviewSession(t *testing.T) {
	s, err := NewService("http://localhost:3000", "http://127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	control, public := s.ControlHandler(), s.PublicHandler()
	runID := uuid.New()
	sessions := "/runs/" + runID.String() + "/sessions"
	do := func(handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(method, target, bytes.NewReader(body)))
		return result
	}
	start := func(content string) Session {
		t.Helper()
		files, err := json.Marshal([]File{
			{Path: "index.html", Content: content},
			{Path: "assets/game.js", Content: "console.log('OnCrew');"},
			{Path: "README.md", Content: "documentation"},
		})
		if err != nil {
			t.Fatal(err)
		}
		result := do(control, http.MethodPost, sessions, files)
		if result.Code != http.StatusCreated {
			t.Fatalf("create preview: %d %s", result.Code, result.Body.String())
		}
		var session Session
		if err := json.Unmarshal(result.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		return session
	}

	first := start("<h1>First</h1>")
	second := start("<h1>Second</h1>")
	page := do(public, http.MethodGet, first.URL, nil)
	if page.Code != http.StatusOK || page.Body.String() != "<h1>First</h1>" {
		t.Fatalf("first snapshot: %d %q", page.Code, page.Body.String())
	}
	for _, directive := range []string{"sandbox allow-scripts", "connect-src 'none'", "frame-ancestors http://localhost:3000", "/p/" + first.ID.String() + "/"} {
		if !strings.Contains(page.Header().Get("Content-Security-Policy"), directive) {
			t.Errorf("missing CSP directive %q", directive)
		}
	}
	if page.Header().Get("Cache-Control") != "no-store" {
		t.Error("preview may be cached")
	}
	assets := strings.TrimSuffix(first.URL, "index.html")
	script := do(public, http.MethodGet, assets+"assets/game.js", nil)
	if script.Code != http.StatusOK || !strings.HasPrefix(script.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("local script: %d %v", script.Code, script.Header())
	}
	if code := do(public, http.MethodGet, assets+"README.md", nil).Code; code != http.StatusNotFound {
		t.Errorf("unexpected status for non-preview artifact: %d", code)
	}
	if code := do(public, http.MethodPost, "http://127.0.0.1:8081"+sessions, nil).Code; code != http.StatusNotFound {
		t.Errorf("control route on public listener: %d", code)
	}
	do(control, http.MethodDelete, "/runs/"+uuid.NewString()+"/sessions/"+first.ID.String(), nil)
	if code := do(public, http.MethodGet, first.URL, nil).Code; code != http.StatusOK {
		t.Errorf("wrong run closed preview: %d", code)
	}
	if code := do(control, http.MethodDelete, sessions+"/"+first.ID.String(), nil).Code; code != http.StatusNoContent {
		t.Fatalf("stop preview: %d", code)
	}
	if code := do(public, http.MethodGet, first.URL, nil).Code; code != http.StatusNotFound {
		t.Errorf("stopped preview still available: %d", code)
	}
	now = now.Add(SessionTTL)
	if code := do(public, http.MethodGet, second.URL, nil).Code; code != http.StatusNotFound {
		t.Errorf("expired preview still available: %d", code)
	}
}

func TestBundleValidation(t *testing.T) {
	entry := File{Path: "index.html"}
	tests := []struct {
		files []File
		err   error
	}{
		{[]File{{Path: "README.md"}}, ErrNoEntry},
		{[]File{entry, entry}, ErrBadBundle},
		{[]File{entry, {Path: "../game.js"}}, ErrBadBundle},
		{[]File{entry, {Path: ".github/script.js"}}, ErrBadBundle},
		{[]File{entry, {Path: "large.js", Content: strings.Repeat(" ", MaxBytes+1)}}, ErrTooLarge},
	}
	for _, tc := range tests {
		if _, err := Bundle(tc.files); !errors.Is(err, tc.err) {
			t.Errorf("got %v, want %v", err, tc.err)
		}
	}
	if _, err := NewService("http://localhost:3000", "http://localhost:8081"); err == nil {
		t.Error("preview accepted the OnCrew hostname")
	}
}
