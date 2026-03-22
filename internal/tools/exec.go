package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ExecTool struct{}

func (t *ExecTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "exec",
		Description: "Execute a shell command and return the output. Use this to run git, npm, go, and other shell commands.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"command": {
					Type:        String,
					Description: "The shell command to execute",
				},
				"timeout": {
					Type:        Number,
					Description: "Timeout in seconds (default: 30)",
					Default:     30,
				},
				"cwd": {
					Type:        String,
					Description: "Working directory for the command",
				},
			},
			Required: []string{"command"},
		},
	}, nil
}

type ExecParams struct {
	Command string  `json:"command"`
	Timeout float64 `json:"timeout"`
	Cwd     string  `json:"cwd"`
}

func (t *ExecTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p ExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	if p.Timeout == 0 {
		p.Timeout = 30
	}

	cmd := exec.Command("cmd", "/C", p.Command)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return &ToolResult{Text: "command cancelled"}, nil
	case err := <-done:
		output := stdout.String()
		if stderr.String() != "" {
			output += "\nSTDERR:\n" + stderr.String()
		}
		if err != nil {
			output += fmt.Sprintf("\nERROR: %v", err)
		}
		return &ToolResult{Text: strings.TrimSpace(output)}, nil
	}
}
