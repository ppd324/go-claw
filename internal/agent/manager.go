package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go-claw/internal/config"
	"go-claw/internal/llm"
	"go-claw/internal/storage"
	"go-claw/internal/tools"
)

type Manager struct {
	cfg            *config.Config
	repo           *storage.Repository
	agents         map[uint]*Agent
	mu             sync.RWMutex
	provider       llm.ModelProvider
	toolRegistry   *tools.ToolRegistry
	contextManager *ContextManager
	sessionManager *SessionManager
	workspace      string
}

func NewManager(cfg *config.Config, repo *storage.Repository, baseDir string) *Manager {
	workspace := baseDir
	if workspace == "" {
		workspace = cfg.WorkDir
	}
	if workspace == "" {
		workspace = "workspace"
	}

	EnsureWorkspace(workspace)

	cm := NewContextManager(workspace)
	cm.Load()

	sm := NewSessionManager(repo)

	m := &Manager{
		cfg:            cfg,
		repo:           repo,
		agents:         make(map[uint]*Agent),
		toolRegistry:   nil,
		contextManager: cm,
		sessionManager: sm,
		workspace:      workspace,
	}

	defaultRegistry := tools.NewDefaultToolRegistry(baseDir)
	m.toolRegistry = defaultRegistry.ToolRegistry

	if p, err := llm.NewOpenAIProvider(cfg); err == nil {
		m.provider = p
	} else if p, err := llm.NewAnthropicProvider(cfg); err == nil {
		m.provider = p
	} else if p, err := llm.NewOllamaProvider(cfg); err == nil {
		m.provider = p
	}

	return m
}

func (m *Manager) CreateAgent(name, description, model, prompt string) (*Agent, error) {
	a := newAgentFromDB(&storage.Agent{
		Name:        name,
		Description: description,
		Model:       model,
		Prompt:      prompt,
		Status:      "active",
		OwnerID:     1,
	}, m.repo, m)
	a.toolRegistry = m.toolRegistry

	dbAgent := a.toDB()
	if err := m.repo.CreateAgent(dbAgent); err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	a.Profile.ID = dbAgent.ID
	a.Profile.CreatedAt = dbAgent.CreatedAt
	a.Profile.UpdatedAt = dbAgent.UpdatedAt

	m.mu.Lock()
	m.agents[a.ID] = a
	m.mu.Unlock()

	return a, nil
}

func (m *Manager) GetAgent(id uint) (*Agent, error) {
	m.mu.RLock()
	a, ok := m.agents[id]
	m.mu.RUnlock()
	if ok {
		return a, nil
	}

	dbAgent, err := m.repo.GetAgent(id)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	a = newAgentFromDB(dbAgent, m.repo, m)
	a.toolRegistry = m.toolRegistry

	m.mu.Lock()
	m.agents[a.ID] = a
	m.mu.Unlock()

	return a, nil
}

func (m *Manager) GetAgentByRoutingKey(routingKey string) (*Agent, error) {
	dbAgent, err := m.repo.GetAgentByRoutingKey(routingKey)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return m.GetAgent(dbAgent.ID)
}

func (m *Manager) ListAgents() ([]*Agent, error) {
	dbAgents, err := m.repo.ListAgents()
	if err != nil {
		return nil, err
	}

	agents := make([]*Agent, 0, len(dbAgents))
	for i := range dbAgents {
		agents = append(agents, newAgentFromDB(&dbAgents[i], m.repo, m))
	}
	return agents, nil
}

func (m *Manager) GetToolRegistry() *tools.ToolRegistry {
	return m.toolRegistry
}

func (m *Manager) GetProvider() llm.ModelProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

func (m *Manager) SetProvider(provider llm.ModelProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = provider
}

func (m *Manager) GetContextManager() *ContextManager {
	return m.contextManager
}

func (m *Manager) GetSessionManager() *SessionManager {
	return m.sessionManager
}

func (m *Manager) GetWorkspace() string {
	return m.workspace
}

func (m *Manager) UpdateAgent(agent *Agent) error {
	return m.repo.UpdateAgent(agent.toDB())
}

func (m *Manager) DeleteAgent(id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.agents[id]; !ok {
		return fmt.Errorf("agent not found")
	}

	if err := m.repo.DeleteAgent(id); err != nil {
		return err
	}

	delete(m.agents, id)
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) {
	slog.Info("shutting down agent manager")

	m.mu.Lock()
	for _, a := range m.agents {
		if err := a.StopAgent(); err != nil {
			slog.Error("failed to stop agent", "agent_name", a.Name, "agent_id", a.ID, "error", err)
		}
	}
	m.mu.Unlock()

	slog.Info("agent manager shutdown complete")
}

func (m *Manager) StartAll(ctx context.Context) error {
	agents, err := m.ListAgents()
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	for _, a := range agents {
		if a.Status == "active" {
			if err := a.StartAgent(ctx); err != nil {
				slog.Error("failed to start agent", "agent_name", a.Name, "agent_id", a.ID, "error", err)
			}
		}
	}

	return nil
}

func (m *Manager) StopAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, a := range m.agents {
		if err := a.StopAgent(); err != nil {
			slog.Error("failed to stop agent", "agent_name", a.Name, "agent_id", a.ID, "error", err)
		}
	}

	return nil
}

func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeCount := 0
	for _, a := range m.agents {
		if a.Status == "active" {
			activeCount++
		}
	}

	return map[string]interface{}{
		"total_agents":  len(m.agents),
		"active_agents": activeCount,
		"workspace":     m.workspace,
		"has_provider":  m.provider != nil,
		"provider_name": func() string {
			if m.provider != nil {
				return m.provider.GetName()
			}
			return ""
		}(),
	}
}

func (m *Manager) SetWorkspace(workspace string) error {
	if err := EnsureWorkspace(workspace); err != nil {
		return err
	}

	m.mu.Lock()
	m.workspace = workspace
	if m.contextManager != nil {
		m.contextManager = NewContextManager(workspace)
		m.contextManager.Load()
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) ReloadContext() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.contextManager != nil {
		if err := m.contextManager.Load(); err != nil {
			return err
		}
	}

	return nil
}
