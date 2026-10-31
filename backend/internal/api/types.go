package api

import "time"

type AgentDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
	Role  string `json:"role"`
}

type AgentRequest struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	BotKey  string `json:"bot_key"`
	Role    string `json:"role"`
}

type RunRequest struct {
	Goal     string   `json:"goal"`
	LeadID   string   `json:"lead_id"`
	AgentIDs []string `json:"agent_ids"`
	SpecText string   `json:"spec_text"`
	SpecName string   `json:"spec_name"`
}

type AnswerRequest struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}

type QuestionDTO struct {
	ID         string  `json:"id"`
	Question   string  `json:"question"`
	Answer     *string `json:"answer"`
	CreatedAt  string  `json:"created_at"`
	AnsweredAt *string `json:"answered_at"`
}

type RunDTO struct {
	ID            string   `json:"id"`
	Goal          string   `json:"goal"`
	Status        string   `json:"status"`
	LeadID        string   `json:"lead_id"`
	AgentIDs      []string `json:"agent_ids"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	TasksTotal    int      `json:"tasks_total"`
	TasksDone     int      `json:"tasks_done"`
	TasksReview   int      `json:"tasks_review"`
	SpecName      string   `json:"spec_name"`
	SpecStatus    string   `json:"spec_status"`
	SpecError     string   `json:"spec_error"`
	FailureReason string   `json:"failure_reason"`
}

type TaskDTO struct {
	ID                  string              `json:"id"`
	Text                string              `json:"text"`
	Status              string              `json:"status"`
	Details             string              `json:"details"`
	Summary             string              `json:"summary"`
	Feedback            string              `json:"feedback"`
	AssigneeID          *string             `json:"assignee_id"`
	ModelPhase          string              `json:"model_phase"`
	ModelStartedAt      *time.Time          `json:"model_started_at"`
	ModelLastActivityAt *time.Time          `json:"model_last_activity_at"`
	FailureReason       string              `json:"failure_reason"`
	Dependencies        []TaskDependencyDTO `json:"dependencies"`
	MemoryStatus        string              `json:"memory_status"`
	MemoryError         string              `json:"memory_error"`
}

type TaskDependencyDTO struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

type ArtifactDTO struct {
	ID       string `json:"id"`
	RunID    string `json:"run_id"`
	TaskID   string `json:"task_id"`
	Filename string `json:"filename"`
	Language string `json:"language"`
	Content  string `json:"content"`
}
