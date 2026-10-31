package agent

import "github.com/yalama34/orchestrator/internal/domain"

type ActionType string

const (
	ActionCreateTasks   ActionType = "create_tasks"
	ActionAssignTask    ActionType = "assign_task"
	ActionAcceptTask    ActionType = "accept_task"
	ActionReworkTask    ActionType = "rework_task"
	ActionUpdateContext ActionType = "update_context"
	ActionMessageUser   ActionType = "message_user"
	ActionAskUser       ActionType = "ask_user"
	ActionCompleteRun   ActionType = "complete_run"
)

type LeadOutput struct {
	Actions []Action `json:"actions"`
}

type WorkerOutput struct {
	Summary   string            `json:"summary"`
	Done      bool              `json:"done"`
	Artifacts []domain.Artifact `json:"artifacts"`
}

type Action struct {
	Type      ActionType `json:"type"`
	Tasks     []TaskSpec `json:"tasks,omitempty"`
	TaskID    string     `json:"task_id,omitempty"`
	AssignTo  string     `json:"assign_to,omitempty"`
	Feedback  string     `json:"feedback,omitempty"`
	ContextMD string     `json:"context_md,omitempty"`
	Text      string     `json:"text,omitempty"`
	Summary   string     `json:"summary,omitempty"`
}

type TaskSpec struct {
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	AssignTo  string   `json:"assign_to"`
	Details   string   `json:"details,omitempty"`
	DependsOn []string `json:"depends_on"`
}
