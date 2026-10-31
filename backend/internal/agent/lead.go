package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yalama34/orchestrator/internal/domain"
)

type Lead struct {
	Agent  domain.Agent
	Client *Client
}

func NewLead(agent domain.Agent, client *Client) Lead {
	lead := new(Lead)
	lead.Agent = agent
	lead.Client = client
	return *lead
}

func Decide(ctx context.Context, lead Lead, system, user string) (LeadOutput, error) {
	out, err := lead.Client.Chat(ctx, lead.Agent, system, user)
	if err != nil {
		return LeadOutput{}, fmt.Errorf("[LEAD] Failed to call model: %w", err)
	}

	out_formatted := new(LeadOutput)
	err = json.Unmarshal([]byte(out), &out_formatted)
	if err != nil {
		return LeadOutput{}, fmt.Errorf("[LEAD] Failed to unmarshal lead output: %w", err)
	}

	return *out_formatted, nil
}
