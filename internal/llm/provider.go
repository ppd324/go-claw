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
	Content      string     `json:"content"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	StopReason   string     `json:"stop_reason"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
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

// StreamHandler handles streaming responses
type StreamHandler interface {
	OnContent(content string)
	OnComplete(response *ChatResponse)
	OnError(err error)
}

// StreamHandlerFunc is a function adapter for StreamHandler
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

// NewStreamHandler creates a StreamHandler from functions
func NewStreamHandler(onContent func(string), onComplete func(*ChatResponse), onError func(error)) StreamHandler {
	return &StreamHandlerFunc{
		OnContentFunc:  onContent,
		OnCompleteFunc: onComplete,
		OnErrorFunc:    onError,
	}
}
