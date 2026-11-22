package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("сервис превью недоступен")
	ErrCapacity    = errors.New("все окна превью заняты, попробуйте позже")
)

type Client struct {
	controlOrigin string
	http          *http.Client
}

func NewClient(controlOrigin string) (*Client, error) {
	if _, err := ParseOrigin(controlOrigin); err != nil {
		return nil, err
	}
	return &Client{controlOrigin: controlOrigin, http: &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Create(ctx context.Context, runID uuid.UUID, bundle []File) (Session, error) {
	var session Session
	body, err := json.Marshal(bundle)
	if err != nil {
		return session, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sessionsURL(runID), bytes.NewReader(body))
	if err != nil {
		return session, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return session, ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
			return Session{}, ErrUnavailable
		}
		return session, nil
	case http.StatusTooManyRequests:
		return session, ErrCapacity
	default:
		return session, ErrUnavailable
	}
}

func (c *Client) Stop(ctx context.Context, runID, id uuid.UUID) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.sessionsURL(runID)+"/"+id.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return ErrUnavailable
	}
	return nil
}

func (c *Client) sessionsURL(runID uuid.UUID) string {
	return c.controlOrigin + "/runs/" + runID.String() + "/sessions"
}
