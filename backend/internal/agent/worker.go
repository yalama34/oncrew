package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
)

type Worker struct {
	Agent        domain.Agent
	Client       *Client
	TaskID       uuid.UUID
	Sources      []KnowledgeSource
	Dependencies []DependencyResult
	Memory       []MemorySource
}

func NewWorker(agent domain.Agent, client *Client, taskID uuid.UUID) Worker {
	worker := Worker{
		Agent:  agent,
		Client: client,
		TaskID: taskID,
	}

	return worker
}

func (w *Worker) Execute(ctx context.Context, taskText, taskDetails, feedback, contextMD string, artifacts []domain.Artifact, questions []domain.RunQuestion, progress ...StreamProgress) (WorkerOutput, error) {
	promptData := WorkerUserPromptData{
		TaskText:     taskText,
		TaskDetails:  taskDetails,
		ContextMD:    contextMD,
		Feedback:     feedback,
		Artifacts:    artifacts,
		Questions:    questions,
		Sources:      w.Sources,
		Dependencies: w.Dependencies,
		Memory:       w.Memory,
	}
	user, err := WorkerUserPrompt(promptData)
	if err != nil {
		return WorkerOutput{}, err
	}
	system := w.Agent.Role.SystemPrompt
	if system == "" {
		system = WorkerSystemPrompt(w.Agent.Role.Name)
	}
	data, err := w.Client.Chat(ctx, w.Agent, system, user, progress...)
	if err != nil {
		return WorkerOutput{}, err
	}
	var out WorkerOutput
	if err = json.Unmarshal([]byte(data), &out); err != nil {
		return out, fmt.Errorf("[WORKER] Failed to unmarshall worker output: %w", err)
	}

	return out, err
}
