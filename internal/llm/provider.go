package llm

import (
	"context"

	"go-claw/internal/tools"
)

type ModelProvider interface {
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest, handler StreamHandler) error
	GetName() string
}

type ChatRequest struct {
	Model        string            `json:"model"`
	SystemPrompt string            `json:"system_prompt"`
	Messages     []Message         `json:"messages"`
	Tools        []*tools.ToolInfo `json:"tools"`
	MaxTokens    int               `json:"max_tokens"`
	Temperature  float64           `json:"temperature"`
}

type ToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatResponse struct {
	ReasonContent string     `json:"reason_content"`
	Content       string     `json:"content"`
	InputTokens   int        `json:"input_tokens"`
	OutputTokens  int        `json:"output_tokens"`
	StopReason    string     `json:"stop_reason"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
}

type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCall   *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"tool_call,omitempty"`
}

type StreamHandler interface {
	OnContent(content string)
	OnComplete(response *ChatResponse)
	OnError(err error)
}

type StreamHandlerFunc struct {
	OnContentFunc  func(string)
	OnCompleteFunc func(*ChatResponse)
	OnErrorFunc    func(error)
}

func (h *StreamHandlerFunc) OnContent(content string) {
	if h.OnContentFunc != nil {
		h.OnContentFunc(content)
	}
}

func (h *StreamHandlerFunc) OnComplete(response *ChatResponse) {
	if h.OnCompleteFunc != nil {
		h.OnCompleteFunc(response)
	}
}

func (h *StreamHandlerFunc) OnError(err error) {
	if h.OnErrorFunc != nil {
		h.OnErrorFunc(err)
	}
}

func NewStreamHandler(onContent func(string), onComplete func(*ChatResponse), onError func(error)) StreamHandler {
	return &StreamHandlerFunc{
		OnContentFunc:  onContent,
		OnCompleteFunc: onComplete,
		OnErrorFunc:    onError,
	}
}

type AgentStreamEvent struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

const (
	EventTypeContent    = "content"
	EventTypeReasoning  = "reasoning"
	EventTypeToolCall   = "tool_call"
	EventTypeToolResult = "tool_result"
	EventTypeComplete   = "complete"
	EventTypeError      = "error"
	EventTypeStart      = "start"
	EventTypeIteration  = "iteration"
)

type ContentEvent struct {
	Content string `json:"content"`
	Delta   bool   `json:"delta"`
}

type ReasoningEvent struct {
	Content string `json:"content"`
}

type ToolCallEvent struct {
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
	CallID   string `json:"call_id"`
}

type ToolResultEvent struct {
	ToolName string `json:"tool_name"`
	Output   string `json:"output"`
	Success  bool   `json:"success"`
}

type CompleteEvent struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	MessageID    string `json:"message_id,omitempty"`
}

type IterationEvent struct {
	Current int `json:"current"`
	Max     int `json:"max"`
}

type AgentStreamHandler interface {
	OnEvent(event *AgentStreamEvent)
}

type AgentStreamHandlerFunc struct {
	OnEventFunc func(*AgentStreamEvent)
}

func (h *AgentStreamHandlerFunc) OnEvent(event *AgentStreamEvent) {
	if h.OnEventFunc != nil {
		h.OnEventFunc(event)
	}
}

func NewAgentStreamHandler(onEvent func(*AgentStreamEvent)) AgentStreamHandler {
	return &AgentStreamHandlerFunc{OnEventFunc: onEvent}
}
