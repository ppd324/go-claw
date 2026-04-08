package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type SubAgentExecutor interface {
	CreateTempAgent(opts *TempAgentOptions) (SubAgentInstance, error)
}

type TempAgentOptions struct {
	Model        string
	AllowedTools []string
	Prompt       string
}

type SubAgentInstance interface {
	Execute(ctx context.Context, input string) (string, int, int, error)
	Cleanup()
}

type SubAgentTool struct {
	executor SubAgentExecutor
}

func NewSubAgentTool(executor SubAgentExecutor) *SubAgentTool {
	return &SubAgentTool{executor: executor}
}

func (t *SubAgentTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name: "subagent",
		Description: `创建临时子 Agent 执行特定任务。

使用场景：
- 搜索关键词或文件，不确定能否一次找到正确匹配
- 执行耗时任务，不阻塞主 Agent
- 需要独立上下文的复杂任务
- 并行执行多个搜索或分析任务

注意：
- 子 Agent 执行完成后自动销毁
- 子 Agent 不能调用 subagent 工具（防止递归）
- 结果不会自动显示给用户，需要你总结后告知用户

最佳实践：
- 任务描述要详细具体，包括期望的输出格式
- 可以在单个消息中调用多个 subagent 进行并行处理
- 子 Agent 的结果应该被信任`,
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"task": {
					Type:        String,
					Description: "要执行的任务，描述要详细具体，包括期望的输出格式",
				},
				"prompt": {
					Type:        String,
					Description: "子 Agent 的系统提示词（可选，用于定义角色和专业领域）",
				},
				"model": {
					Type:        String,
					Description: "指定模型（可选，默认继承主 Agent 模型）",
				},
				"timeout": {
					Type:        Number,
					Description: "超时秒数（可选，默认 300 秒）",
				},
				"tools": {
					Type:        Array,
					Description: "限制可用的工具列表（可选，默认继承主 Agent 工具但排除 subagent）",
				},
			},
			Required: []string{"task"},
		},
	}, nil
}

func (t *SubAgentTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var args struct {
		Task    string   `json:"task"`
		Prompt  string   `json:"prompt,omitempty"`
		Model   string   `json:"model,omitempty"`
		Timeout int      `json:"timeout,omitempty"`
		Tools   []string `json:"tools,omitempty"`
	}

	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if args.Task == "" {
		return nil, fmt.Errorf("task is required")
	}

	timeout := 300 * time.Second
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	instance, err := t.executor.CreateTempAgent(&TempAgentOptions{
		Model:        args.Model,
		AllowedTools: args.Tools,
		Prompt:       args.Prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sub-agent: %w", err)
	}
	defer instance.Cleanup()

	content, inputTokens, outputTokens, err := instance.Execute(ctx, args.Task)
	if err != nil {
		return nil, fmt.Errorf("sub-agent execution failed: %w", err)
	}

	output := content
	if inputTokens > 0 || outputTokens > 0 {
		output += fmt.Sprintf("\n\n[Token: %d 输入 / %d 输出]", inputTokens, outputTokens)
	}

	return &ToolResult{
		Text: output,
	}, nil
}
