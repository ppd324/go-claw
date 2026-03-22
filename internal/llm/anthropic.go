package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go-claw/internal/config"
)

// AnthropicProvider implements ModelProvider for Anthropic Claude API
type AnthropicProvider struct {
	client    *http.Client
	apiKey    string
	model     string
	maxTokens int
	baseURL   string
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(cfg *config.Config) (*AnthropicProvider, error) {
	if cfg.LLMProvider.ApiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	return &AnthropicProvider{
		client:    &http.Client{Timeout: cfg.LLMProvider.Timeout},
		apiKey:    cfg.LLMProvider.ApiKey,
		model:     cfg.LLMProvider.Model,
		maxTokens: cfg.LLMProvider.MaxTokens,
		baseURL:   "https://api.anthropic.com",
	}, nil
}

// Chat sends a chat request to Claude
func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.maxTokens
	}

	// Build request body
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   req.Messages,
	}

	if req.SystemPrompt != "" {
		payload["system"] = req.SystemPrompt
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Build request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Send request
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(respBody))
	}

	// Parse response
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response error: %w", err)
	}

	// Extract content
	var content string
	for _, c := range result.Content {
		content += c.Text
	}

	return &ChatResponse{
		Content:      content,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		StopReason:   result.StopReason,
	}, nil
}

// ChatStream sends a chat request and streams the response
func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatRequest, handler StreamHandler) error {
	model := req.Model
	if model == "" {
		model = p.model
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.maxTokens
	}

	// Build request body
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   req.Messages,
		"stream":     true,
	}

	if req.SystemPrompt != "" {
		payload["system"] = req.SystemPrompt
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	body, _ := json.Marshal(payload)

	// Build request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		handler.OnError(err)
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Send request
	resp, err := p.client.Do(httpReq)
	if err != nil {
		handler.OnError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("API error: %s", string(respBody))
		handler.OnError(err)
		return err
	}

	// Read streaming response
	decoder := json.NewDecoder(resp.Body)
	var content string
	var inputTokens, outputTokens int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}

		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			handler.OnError(err)
			return err
		}

		switch event.Type {
		case "content_block_delta":
			content += event.Delta.Text
			handler.OnContent(event.Delta.Text)
		case "message_delta":
			outputTokens = event.Usage.OutputTokens
		case "message_start":
			inputTokens = event.Usage.InputTokens
		}
	}

	handler.OnComplete(&ChatResponse{
		Content:      content,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		StopReason:   "stop",
	})

	return nil
}

// GetName returns the provider name
func (p *AnthropicProvider) GetName() string {
	return "anthropic"
}
