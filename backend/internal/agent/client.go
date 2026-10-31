package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yalama34/orchestrator/internal/domain"
)

var (
	ErrFirstContentTimeout = errors.New("timed out waiting for first model content")
	ErrStreamIdleTimeout   = errors.New("model stream stopped producing content")
	ErrStreamTotalTimeout  = errors.New("model request exceeded total time limit")
	ErrRequestRejected     = errors.New("model provider rejected the request")
)

type Client struct {
	http                                    *http.Client
	firstTimeout, idleTimeout, totalTimeout time.Duration
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type StreamProgress func(time.Time)

type streamEvent struct {
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningDetails []struct {
				Text    string `json:"text"`
				Summary string `json:"summary"`
			} `json:"reasoning_details"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func NewClient(h *http.Client) *Client {
	return &Client{http: h, firstTimeout: 60 * time.Second, idleTimeout: 60 * time.Second, totalTimeout: 10 * time.Minute}
}

func (c *Client) Chat(ctx context.Context, agent domain.Agent, system, user string, progress ...StreamProgress) (string, error) {
	body, err := json.Marshal(chatRequest{Model: agent.Model, Stream: true, Messages: []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}})
	if err != nil {
		return "", fmt.Errorf("[CLIENT] Failed to marshal request: %w", err)
	}
	totalCtx, totalCancel := context.WithTimeoutCause(ctx, c.totalTimeout, ErrStreamTotalTimeout)
	defer totalCancel()
	streamCtx, cancel := context.WithCancelCause(totalCtx)
	defer cancel(nil)
	activity := make(chan struct{}, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		timer := time.NewTimer(c.firstTimeout)
		defer timer.Stop()
		seen := false
		for {
			select {
			case <-done:
				return
			case <-streamCtx.Done():
				return
			case <-activity:
				seen = true
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(c.idleTimeout)
			case <-timer.C:
				if seen {
					cancel(ErrStreamIdleTimeout)
				} else {
					cancel(ErrFirstContentTimeout)
				}
				return
			}
		}
	}()
	markActivity := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
		if len(progress) > 0 && progress[0] != nil {
			progress[0](time.Now().UTC())
		}
	}
	requestURL := strings.TrimRight(agent.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("[CLIENT] Failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+agent.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		if cause := context.Cause(streamCtx); cause != nil {
			return "", cause
		}
		return "", fmt.Errorf("[CLIENT] Failed to get response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge {
			return "", fmt.Errorf("%w: HTTP %d", ErrRequestRejected, resp.StatusCode)
		}
		return "", fmt.Errorf("[CLIENT] API returned HTTP %d", resp.StatusCode)
	}
	reader := bufio.NewReader(io.LimitReader(resp.Body, 32<<20))
	var content strings.Builder
	var data []string
	finished := false
	completion := false
	handleEvent := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			completion = true
			return nil
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return fmt.Errorf("[CLIENT] Invalid stream event: %w", err)
		}
		if len(event.Error) > 0 && string(event.Error) != "null" {
			return errors.New("[CLIENT] Provider returned a stream error")
		}
		if len(event.Choices) == 0 {
			return nil
		}
		choice := event.Choices[0]
		meaningful := choice.Delta.Content != "" || choice.Delta.Reasoning != ""
		for _, detail := range choice.Delta.ReasoningDetails {
			if detail.Text != "" || detail.Summary != "" {
				meaningful = true
			}
		}
		if meaningful {
			markActivity()
		}
		content.WriteString(choice.Delta.Content)
		if choice.FinishReason != nil {
			if *choice.FinishReason != "stop" {
				return fmt.Errorf("[CLIENT] Model stopped with reason %q", *choice.FinishReason)
			}
			finished = true
		}
		return nil
	}
	for !completion {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if line == "" {
				if err := handleEvent(); err != nil {
					return "", err
				}
			} else if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if readErr != nil {
			if cause := context.Cause(streamCtx); cause != nil {
				return "", cause
			}
			if readErr != io.EOF {
				return "", fmt.Errorf("[CLIENT] Failed to read stream: %w", readErr)
			}
			return "", errors.New("[CLIENT] Stream ended before [DONE]")
		}
	}
	if cause := context.Cause(streamCtx); cause != nil {
		return "", cause
	}
	if !finished {
		return "", errors.New("[CLIENT] Stream ended without a successful finish reason")
	}
	if content.Len() == 0 {
		return "", errors.New("[CLIENT] Model returned empty content")
	}
	return content.String(), nil
}
