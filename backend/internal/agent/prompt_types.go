package agent

import "github.com/yalama34/orchestrator/internal/domain"

type WorkerUserPromptData struct {
	TaskText     string
	TaskDetails  string
	ContextMD    string
	Feedback     string
	Artifacts    []domain.Artifact
	Questions    []domain.RunQuestion
	Sources      []KnowledgeSource
	Dependencies []DependencyResult
	Memory       []MemorySource
}

type MemorySource struct {
	TaskID    string
	TaskTitle string
	Source    string
	Filename  string
	Content   string
}

type DependencyResult struct {
	Title     string
	Summary   string
	Artifacts []domain.Artifact
}

type KnowledgeSource struct {
	Name    string
	Heading string
	Content string
}

type LeadUserPromptData struct {
	Goal      string
	ContextMD string
	Tasks     []LeadTaskRow
	Results   []LeadResultRow
	Roles     []string
	Questions []domain.RunQuestion
	SpecText  string
	SpecName  string
}

type LeadTaskRow struct {
	ID        string
	Status    string
	Assignee  string
	Text      string
	Details   string
	Feedback  string
	DependsOn []string
}

type LeadResultRow struct {
	TaskID    string
	TaskText  string
	Summary   string
	Done      bool
	Artifacts []domain.Artifact
	Sources   []KnowledgeSource
	Memory    []MemorySource
}
