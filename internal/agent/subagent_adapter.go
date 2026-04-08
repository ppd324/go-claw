package agent

import (
	"context"

	"go-claw/internal/tools"
)

type SubAgentAdapter struct {
	manager *Manager
}

func NewSubAgentAdapter(manager *Manager) *SubAgentAdapter {
	return &SubAgentAdapter{manager: manager}
}

func (a *SubAgentAdapter) CreateTempAgent(opts *tools.TempAgentOptions) (tools.SubAgentInstance, error) {
	agentOpts := &TempAgentOptions{
		Model:        opts.Model,
		AllowedTools: opts.AllowedTools,
		Prompt:       opts.Prompt,
	}

	agent, cleanup, err := a.manager.CreateTempAgent(agentOpts)
	if err != nil {
		return nil, err
	}

	return &SubAgentInstanceAdapter{
		agent:   agent,
		cleanup: cleanup,
	}, nil
}

type SubAgentInstanceAdapter struct {
	agent   *Agent
	cleanup func()
}

func (a *SubAgentInstanceAdapter) Execute(ctx context.Context, input string) (string, int, int, error) {
	result, err := a.agent.Execute(ctx, ExecuteRequest{
		Input:            input,
		SaveInputMessage: false,
		ContextEmpty:     true,
	})
	if err != nil {
		return "", 0, 0, err
	}

	return result.Content, result.InputTokens, result.OutputTokens, nil
}

func (a *SubAgentInstanceAdapter) Cleanup() {
	if a.cleanup != nil {
		a.cleanup()
	}
}
