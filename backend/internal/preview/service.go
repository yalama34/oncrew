package preview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	SessionTTL  = 15 * time.Minute
	MaxSessions = 8
)

type Session struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type snapshot struct {
	runID     uuid.UUID
	expiresAt time.Time
	files     map[string]string
}

type Service struct {
	appOrigin    string
	publicOrigin *url.URL
	now          func() time.Time
	mu           sync.Mutex
	sessions     map[uuid.UUID]snapshot
}

func NewService(appOrigin, publicOrigin string) (*Service, error) {
	app, appErr := ParseOrigin(appOrigin)
	public, publicErr := ParseOrigin(publicOrigin)
	if appErr != nil || publicErr != nil || strings.EqualFold(app.Hostname(), public.Hostname()) {
		return nil, errors.New("preview and OnCrew must use valid origins with distinct hostnames")
	}
	return &Service{appOrigin: appOrigin, publicOrigin: public, now: time.Now, sessions: make(map[uuid.UUID]snapshot)}, nil
}

func (s *Service) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /runs/{run}/sessions", s.create)
	mux.HandleFunc("DELETE /runs/{run}/sessions/{session}", s.stop)
	return mux
}

func (s *Service) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p/{session}/{file...}", s.serveFile)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), clipboard-read=(), clipboard-write=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
		if !strings.EqualFold(r.Host, s.publicOrigin.Host) {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(r.PathValue("run"))
	if err != nil {
		http.Error(w, "некорректный запуск", http.StatusBadRequest)
		return
	}
	var files []File
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBytes))
	if err := decoder.Decode(&files); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, ErrTooLarge.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "некорректный набор файлов", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		http.Error(w, "некорректный набор файлов", http.StatusBadRequest)
		return
	}
	bundle, err := Bundle(files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	now := s.now()
	id := uuid.New()
	expiresAt := now.Add(SessionTTL)
	content := make(map[string]string, len(bundle))
	for _, file := range bundle {
		content[file.Path] = file.Content
	}

	s.mu.Lock()
	s.removeExpired(now)
	if len(s.sessions) >= MaxSessions {
		s.mu.Unlock()
		http.Error(w, "все места превью заняты, закройте одно из окон и повторите попытку", http.StatusTooManyRequests)
		return
	}
	s.sessions[id] = snapshot{runID: runID, expiresAt: expiresAt, files: content}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(Session{ID: id, URL: s.publicOrigin.String() + "/p/" + id.String() + "/index.html", ExpiresAt: expiresAt})
}

func (s *Service) stop(w http.ResponseWriter, r *http.Request) {
	runID, runErr := uuid.Parse(r.PathValue("run"))
	id, sessionErr := uuid.Parse(r.PathValue("session"))
	if runErr != nil || sessionErr != nil {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	entry, ok := s.sessions[id]
	if ok && entry.runID == runID {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) serveFile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("session"))
	name := r.PathValue("file")
	mime := ContentType(name)
	if err != nil || id.String() != r.PathValue("session") || !validPath(name) || mime == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.Lock()
	entry, found := s.sessions[id]
	content, ok := entry.files[name]
	if found && !s.now().Before(entry.expiresAt) {
		delete(s.sessions, id)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	source := s.publicOrigin.String() + "/p/" + id.String() + "/"
	w.Header().Set("Content-Security-Policy", fmt.Sprintf(
		"default-src 'none'; sandbox allow-scripts; frame-ancestors %s; base-uri 'none'; form-action 'none'; "+
			"script-src %s 'unsafe-inline'; style-src %s 'unsafe-inline'; img-src %s data:; media-src %s; connect-src 'none'",
		s.appOrigin, source, source, source, source))
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, content)
	}
}

// removeExpired is called with the session mutex held.
func (s *Service) removeExpired(now time.Time) {
	for id, entry := range s.sessions {
		if !now.Before(entry.expiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *Service) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(s.now())
}
