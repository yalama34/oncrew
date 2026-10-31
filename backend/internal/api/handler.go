package api

import (
	"github.com/yalama34/orchestrator/internal/engine"
	"github.com/yalama34/orchestrator/internal/store"
)

type AgentHandler struct {
	Store *store.Store
}

type ArtifactHandler struct {
	Store *store.Store
}

type RunHandler struct {
	Engine *engine.Engine
	Store  *store.Store
}

type TaskHandler struct {
	Store *store.Store
}
