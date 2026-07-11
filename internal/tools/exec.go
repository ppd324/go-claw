package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ExecTool struct{}

func (t *ExecTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "exec",
		Description: "Execute a command and return the output. Use either `program` plus `args` or `command`, but not both. Prefer `program` plus `args` for Python, Go, Node, and Windows paths.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"program": {
					Type:        String,
					Description: "Executable to run directly without shell parsing. For Python scripts, set `program` to `python` and put the script path in `args[0]`.",
				},
				"args": {
					Type:        Array,
					Description: "Arguments for `program`, passed without shell parsing. Example: [\"script.py\", \"--flag\", \"value\"].",
				},
				"command": {
					Type:        String,
					Description: "The shell command to execute. Use only when shell features are required.",
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
			Required: []string{},
		},
	}, nil
}

type ExecParams struct {
	Command string   `json:"command"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Timeout float64  `json:"timeout"`
	Cwd     string   `json:"cwd"`
}

func (t *ExecTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p ExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	if p.Timeout == 0 {
		p.Timeout = 30
	}

	if err := validateExecParams(p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to execute command: %v", err)}, nil
	}

	program, args, err := normalizeProgramArgs(p.Program, p.Args)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to execute command: %v", err)}, nil
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(p.Timeout*float64(time.Second)))
	defer cancel()

	var cmd *exec.Cmd
	switch {
	case strings.TrimSpace(program) != "":
		cmd = exec.CommandContext(execCtx, program, args...)
	case strings.TrimSpace(p.Command) != "":
		cmd = exec.CommandContext(execCtx, "cmd", "/C", p.Command)
	default:
		return &ToolResult{Text: "failed to execute command: either `program` or `command` is required"}, nil
	}

	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := stdout.String()
	if stderr.String() != "" {
		output += "\nSTDERR:\n" + stderr.String()
	}
	if execCtx.Err() == context.DeadlineExceeded {
		output += fmt.Sprintf("\nERROR: command timed out after %.0f seconds", p.Timeout)
	} else if execCtx.Err() == context.Canceled {
		output += "\nERROR: command cancelled"
	} else if err != nil {
		output += fmt.Sprintf("\nERROR: %v", err)
	}

	return &ToolResult{Text: strings.TrimSpace(output)}, nil
}

func validateExecParams(p ExecParams) error {
	hasProgram := strings.TrimSpace(p.Program) != ""
	hasCommand := strings.TrimSpace(p.Command) != ""

	switch {
	case hasProgram && hasCommand:
		return fmt.Errorf("provide either `program` plus `args` or `command`, not both")
	case !hasProgram && len(p.Args) > 0:
		return fmt.Errorf("`args` requires `program`")
	case !hasProgram && !hasCommand:
		return fmt.Errorf("either `program` or `command` is required")
	default:
		return nil
	}
}

func normalizeProgramArgs(program string, args []string) (string, []string, error) {
	trimmed := strings.TrimSpace(program)
	if trimmed == "" {
		return trimmed, args, nil
	}

	switch strings.ToLower(filepath.Ext(trimmed)) {
	case ".py", ".pyw":
		python, err := resolvePythonInterpreter()
		if err != nil {
			return "", nil, err
		}
		normalizedArgs := make([]string, 0, len(args)+1)
		normalizedArgs = append(normalizedArgs, trimmed)
		normalizedArgs = append(normalizedArgs, args...)
		return python, normalizedArgs, nil
	default:
		return trimmed, args, nil
	}
}

func resolvePythonInterpreter() (string, error) {
	candidates := []string{"python"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "py")
	} else {
		candidates = append(candidates, "python3")
	}

	for _, candidate := range candidates {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("python interpreter not found on PATH")
}
