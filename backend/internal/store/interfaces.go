package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
)

type AgentStore interface {
	CreateAgent(ctx context.Context, agent *domain.Agent) error
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
	GetAgentByRole(ctx context.Context, role domain.RoleName) (domain.Agent, error)
	ListAgentsByRun(ctx context.Context, runID uuid.UUID) ([]domain.Agent, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, run *domain.Run) error
	GetRun(ctx context.Context, id uuid.UUID) (domain.Run, error)
	GetAllRuns(ctx context.Context) ([]domain.Run, error)
	UpdateRunStatus(ctx context.Context, id uuid.UUID, status domain.RunStatus) error
	SetRunAgents(ctx context.Context, id uuid.UUID, agents []domain.Agent) error
}

type TaskStore interface {
	CreateTask(ctx context.Context, task *domain.Task) error
	GetTask(ctx context.Context, id uuid.UUID) (domain.Task, error)
	UpdateTaskStatus(ctx context.Context, id uuid.UUID, status domain.TaskStatus) error
	SetTaskFeedback(ctx context.Context, id uuid.UUID, feedback string) error
	AssignTask(ctx context.Context, id uuid.UUID, assignee *domain.Agent) error
	ListTasksByRun(ctx context.Context, runID uuid.UUID) ([]domain.Task, error)
}

type ContextStore interface {
	UpsertContext(ctx context.Context, c domain.RunContext) error
	GetContext(ctx context.Context, runID uuid.UUID) (domain.RunContext, error)
}

type ArtifactStore interface {
	CreateArtifact(ctx context.Context, artifact *domain.Artifact) error
	GetArtifact(ctx context.Context, filename string) (domain.Artifact, error)
	GetArtifactById(ctx context.Context, id uuid.UUID) (domain.Artifact, error)
	ListArtifactsByRun(ctx context.Context, runID uuid.UUID) ([]domain.Artifact, error)
	UpdateArtifact(ctx context.Context, filename, content string) error
}
