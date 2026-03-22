package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go-claw/internal/llm"
	"go-claw/internal/storage"
	"go-claw/internal/tools"
)

const (
	defaultSystemPrompt = "You are a helpful AI assistant."
	MaxToolIterations   = 20
)

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema string `json:"input_schema"`
}

type Profile struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Model       string    `json:"model"`
	Prompt      string    `json:"prompt"`
	Tools       []Tool    `json:"tools"`
	Skills      []string  `json:"skills"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RuntimeState struct {
	ProviderName       string    `json:"provider_name"`
	ResolvedModel      string    `json:"resolved_model"`
	SystemPrompt       string    `json:"system_prompt"`
	ConversationTurns  int       `json:"conversation_turns"`
	LastSessionID      uint      `json:"last_session_id"`
	LastUserMessage    string    `json:"last_user_message,omitempty"`
	LastAssistantReply string    `json:"last_assistant_reply,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
	LastRunAt          time.Time `json:"last_run_at,omitempty"`
}

type Snapshot struct {
	Profile Profile      `json:"profile"`
	Runtime RuntimeState `json:"runtime"`
}

type ExecuteRequest struct {
	SessionID        uint
	Input            string
	InputRole        string
	Mode             ExecutionMode
	SaveInputMessage bool // Whether to save user message to database (default: true)
}

type ExecuteResult struct {
	Content      string           `json:"content"`
	InputTokens  int              `json:"input_tokens"`
	OutputTokens int              `json:"output_tokens"`
	StopReason   string           `json:"stop_reason,omitempty"`
	ToolCalls    []ToolCallResult `json:"tool_calls,omitempty"`
}

type ExecutionMode string

const (
	ModeNormal ExecutionMode = "normal"
	ModePlan   ExecutionMode = "plan"
)

type ToolCallResult struct {
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
	Output   string `json:"output"`
	Success  bool   `json:"success"`
}

type Agent struct {
	Profile
	mu           sync.RWMutex
	runtime      RuntimeState
	repo         *storage.Repository
	manager      *Manager
	toolRegistry *tools.ToolRegistry
}

func (a *Agent) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	// Add session_id to context for tool calls
	ctx = context.WithValue(ctx, "session_id", req.SessionID)
	ctx = context.WithValue(ctx, "agent_id", a.ID)

	provider := a.manager.GetProvider()
	if provider == nil {
		err := fmt.Errorf("no llm provider configured")
		a.markRunError(req, err)
		return nil, err
	}

	history, err := a.getConversationHistory(req.SessionID)
	if err != nil {
		a.markRunError(req, err)
		return nil, err
	}

	messages := make([]llm.Message, 0, len(history)+1)
	for _, msg := range history {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		messages = append(messages, llm.Message{
			Role:    role,
			Content: msg.Content,
		})
	}
	if req.InputRole == "" {
		req.InputRole = "user"
	}
	messages = append(messages, llm.Message{
		Role:    req.InputRole,
		Content: req.Input,
	})

	systemPrompt := a.systemPrompt()
	model := a.getModel()
	toolList := a.toolRegistry.List()

	result := &ExecuteResult{}

	for iteration := 0; iteration < MaxToolIterations; iteration++ {
		chatReq := &llm.ChatRequest{
			Model:        model,
			SystemPrompt: systemPrompt,
			Messages:     messages,
			MaxTokens:    a.manager.cfg.LLMProvider.MaxTokens,
		}

		if len(toolList) > 0 {
			chatReq.Tools = toolList
		}

		resp, err := provider.Chat(ctx, chatReq)
		if err != nil {
			a.markRunError(req, err)
			return nil, fmt.Errorf("model API error: %w", err)
		}

		result.InputTokens += resp.InputTokens
		result.OutputTokens += resp.OutputTokens

		if iteration == 0 {
			result.Content = resp.Content
		}

		if len(resp.ToolCalls) == 0 {
			if iteration > 0 {
				result.Content = resp.Content
			}
			break
		}

		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		toolResults, err := a.executeToolCalls(ctx, resp.ToolCalls)
		if err != nil {
			a.markRunError(req, err)
			return nil, err
		}

		result.ToolCalls = append(result.ToolCalls, toolResults...)

		for _, tc := range resp.ToolCalls {
			for _, tr := range toolResults {
				if tr.ToolName == tc.Function.Name {
					messages = append(messages, llm.Message{
						Role:       "tool",
						Content:    tr.Output,
						ToolCallID: tc.ID,
					})
					break
				}
			}
		}
	}

	// Save user message and assistant response
	// Default behavior: save messages (when SaveMessage is not explicitly set to false)
	if err := a.saveMessages(req.SessionID, req.Input, result.Content, req.SaveInputMessage); err != nil {
		slog.Warn("failed to save messages", "session_id", req.SessionID, "error", err)
	}

	// Save tool calls regardless of SaveMessage flag
	if len(result.ToolCalls) > 0 {
		if err := a.saveToolCalls(req.SessionID, result.ToolCalls); err != nil {
			slog.Warn("failed to save tool calls", "session_id", req.SessionID, "error", err)
		}
	}

	a.markRunSuccess(req, result, provider.GetName(), model, len(history))
	return result, nil
}

func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall) ([]ToolCallResult, error) {
	results := make([]ToolCallResult, 0, len(toolCalls))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, tc := range toolCalls {
		result := ToolCallResult{
			ToolName: tc.Function.Name,
			Input:    tc.Function.Arguments,
		}
		wg.Add(1)
		go func(result ToolCallResult) {
			defer func() {
				wg.Done()
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}()
			tool, ok := a.toolRegistry.Get(tc.Function.Name)
			if !ok {
				result.Output = fmt.Sprintf("tool %s not found", tc.Function.Name)
				result.Success = false
				return
			}

			toolResult, err := tool.Invoke(ctx, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				result.Output = fmt.Sprintf("error: %v", err)
				result.Success = false
			} else {
				result.Output = toolResult.Text
				result.Success = true
			}
		}(result)
	}
	wg.Wait()

	return results, nil
}

func (a *Agent) CreateSession(ctx context.Context) (*storage.Session, error) {
	session, err := a.manager.sessionManager.CreateSession(1, a.ID, "New Conversation", "cli")
	if err != nil {
		slog.Error("failed to create session", "agent_id", a.ID, "error", err)
		return nil, err
	}
	return session, nil
}

func (a *Agent) ProcessMessage(ctx context.Context, content string, sessionID uint) (string, error) {
	result, err := a.Execute(ctx, ExecuteRequest{
		SessionID: sessionID,
		Input:     content,
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (a *Agent) ProcessMessageStream(ctx context.Context, content string, sessionID uint, handler llm.StreamHandler) error {
	provider := a.manager.GetProvider()
	if provider == nil {
		err := fmt.Errorf("no llm provider configured")
		a.markRunError(ExecuteRequest{SessionID: sessionID, Input: content}, err)
		handler.OnError(err)
		return err
	}

	history, err := a.getConversationHistory(sessionID)
	if err != nil {
		a.markRunError(ExecuteRequest{SessionID: sessionID, Input: content}, err)
		handler.OnError(err)
		return err
	}

	chatMessages := make([]llm.Message, 0, len(history)+1)
	for _, msg := range history {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		chatMessages = append(chatMessages, llm.Message{
			Role:    role,
			Content: msg.Content,
		})
	}
	chatMessages = append(chatMessages, llm.Message{Role: "user", Content: content})

	model := a.getModel()

	chatReq := &llm.ChatRequest{
		Model:        model,
		SystemPrompt: a.systemPrompt(),
		Messages:     chatMessages,
		MaxTokens:    a.manager.cfg.LLMProvider.MaxTokens,
	}

	toolList := a.toolRegistry.List()
	if len(toolList) > 0 {
		chatReq.Tools = toolList
	}

	return provider.ChatStream(ctx, chatReq, handler)
}

func (a *Agent) AddTool(tool tools.InvokeTool) error {
	return a.toolRegistry.Register(tool)
}

func (a *Agent) RemoveTool(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, tool := range a.Tools {
		if tool.Name == name {
			a.Tools = append(a.Tools[:i], a.Tools[i+1:]...)
			return
		}
	}
}

func (a *Agent) StartAgent(ctx context.Context) error {
	a.mu.Lock()
	a.Status = "active"
	a.mu.Unlock()

	go func() {
		<-ctx.Done()
		a.mu.Lock()
		a.Status = "stopped"
		a.mu.Unlock()
	}()
	return nil
}

func (a *Agent) StopAgent() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Status = "stopped"
	return nil
}

func (a *Agent) Ping() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status == "active"
}

func (a *Agent) GetStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status
}

func (a *Agent) Snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	runtime := a.runtime
	runtime.SystemPrompt = a.systemPrompt()
	runtime.ResolvedModel = a.getModel()
	if runtime.ProviderName == "" && a.manager.GetProvider() != nil {
		runtime.ProviderName = a.manager.GetProvider().GetName()
	}

	return Snapshot{
		Profile: a.Profile,
		Runtime: runtime,
	}
}

func (a *Agent) getConversationHistory(sessionID uint) ([]storage.Message, error) {
	if sessionID == 0 {
		return nil, nil
	}
	return a.manager.sessionManager.GetMessages(sessionID)
}

func (a *Agent) saveMessages(sessionID uint, userInput, assistantOutput string, saveInput bool) error {
	if sessionID == 0 {
		return nil
	}

	sm := a.manager.sessionManager
	if sm == nil {
		return nil
	}
	if saveInput {

		if _, err := sm.AddMessage(sessionID, "user", userInput); err != nil {
			return err
		}
	}
	if assistantOutput != "" {
		if _, err := sm.AddMessage(sessionID, "assistant", assistantOutput); err != nil {
			return err
		}
	}

	return nil
}

func (a *Agent) saveToolCalls(sessionID uint, toolCalls []ToolCallResult) error {
	if sessionID == 0 || len(toolCalls) == 0 {
		return nil
	}

	// 获取 session 信息
	session, err := a.repo.GetSession(sessionID)
	if err != nil {
		return err
	}

	// 获取最后一条消息（assistant 消息）
	messages, err := a.repo.GetMessagesBySession(sessionID)
	if err != nil || len(messages) == 0 {
		return fmt.Errorf("no messages found for session")
	}

	// 找到最后一条 assistant 消息
	var lastAssistantMsg *storage.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantMsg = &messages[i]
			break
		}
	}

	if lastAssistantMsg == nil {
		return fmt.Errorf("no assistant message found")
	}

	// 为每个工具调用创建记录，关联到 assistant 消息
	for _, tc := range toolCalls {
		trace := &storage.ToolCallTrace{
			SessionIDRef: sessionID,
			AgentID:      session.AgentID,
			MessageID:    lastAssistantMsg.ID, // 关联到 assistant 消息
			ToolName:     tc.ToolName,
			CallID:       fmt.Sprintf("call_%d", time.Now().UnixNano()),
			ToolInput:    tc.Input,
			ToolOutput:   tc.Output,
			Success:      tc.Success,
		}
		if err := a.repo.CreateToolCallTrace(trace); err != nil {
			slog.Warn("failed to save tool call trace", "session_id", sessionID, "tool_name", tc.ToolName, "error", err)
		}
	}

	return nil
}

func (a *Agent) getModel() string {
	if a.Model != "" {
		return a.Model
	}
	if a.manager.cfg.LLMProvider.Model != "" {
		return a.manager.cfg.LLMProvider.Model
	}
	return "gpt-4"
}

func (a *Agent) systemPrompt() string {
	var sb strings.Builder

	if cm := a.manager.GetContextManager(); cm != nil {
		workspacePrompt := cm.BuildSystemPrompt()
		if workspacePrompt != "" {
			sb.WriteString(workspacePrompt)
		}
	}

	prompt := a.Prompt
	if prompt != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(prompt)
	}

	toolNames := a.toolRegistry.ListNames()
	if len(toolNames) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("You have access to the following tools:\n")
		for _, name := range toolNames {
			if info, ok := a.toolRegistry.GetInfo(name); ok {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", info.Name, info.Description))
			}
		}
	}

	if sb.Len() == 0 {
		return defaultSystemPrompt
	}
	return sb.String()
}

func (a *Agent) toDB() *storage.Agent {
	return &storage.Agent{
		ID:          a.ID,
		Name:        a.Name,
		Description: a.Description,
		Model:       a.Model,
		Prompt:      a.Prompt,
		Tools:       mustMarshalTools(a.Tools),
		Skills:      mustMarshalSkills(a.Skills),
		Status:      a.Status,
		State:       mustMarshalRuntime(a.runtime),
		OwnerID:     1,
	}
}

func (a *Agent) markRunSuccess(req ExecuteRequest, result *ExecuteResult, providerName, model string, historySize int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.runtime.ProviderName = providerName
	a.runtime.ResolvedModel = model
	a.runtime.SystemPrompt = a.systemPrompt()
	a.runtime.ConversationTurns = historySize
	a.runtime.LastSessionID = req.SessionID
	a.runtime.LastUserMessage = req.Input
	a.runtime.LastAssistantReply = result.Content
	a.runtime.LastError = ""
	a.runtime.LastRunAt = time.Now()
}

func (a *Agent) markRunError(req ExecuteRequest, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.runtime.LastSessionID = req.SessionID
	a.runtime.LastUserMessage = req.Input
	a.runtime.LastError = err.Error()
	a.runtime.LastRunAt = time.Now()
}

func newAgentFromDB(dbAgent *storage.Agent, repo *storage.Repository, manager *Manager) *Agent {
	tools := make([]Tool, 0)
	skills := make([]string, 0)
	runtime := RuntimeState{}

	_ = json.Unmarshal([]byte(dbAgent.Tools), &tools)
	_ = json.Unmarshal([]byte(dbAgent.Skills), &skills)
	_ = json.Unmarshal([]byte(dbAgent.State), &runtime)

	return &Agent{
		Profile: Profile{
			ID:          dbAgent.ID,
			Name:        dbAgent.Name,
			Description: dbAgent.Description,
			Model:       dbAgent.Model,
			Prompt:      dbAgent.Prompt,
			Tools:       tools,
			Skills:      skills,
			Status:      dbAgent.Status,
			CreatedAt:   dbAgent.CreatedAt,
			UpdatedAt:   dbAgent.UpdatedAt,
		},
		runtime:      runtime,
		repo:         repo,
		manager:      manager,
		toolRegistry: manager.toolRegistry,
	}
}

func mustMarshalTools(tools []Tool) string {
	if len(tools) == 0 {
		return "[]"
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func mustMarshalSkills(skills []string) string {
	if len(skills) == 0 {
		return "[]"
	}
	data, err := json.Marshal(skills)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func mustMarshalRuntime(runtime RuntimeState) string {
	data, err := json.Marshal(runtime)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (a *Agent) SetToolRegistry(registry *tools.ToolRegistry) {
	a.toolRegistry = registry
}

func parseOutputTokens(content string) int {
	lines := strings.Split(content, "\n")
	return len(lines)
}
