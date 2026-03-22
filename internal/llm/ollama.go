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

// OllamaProvider implements ModelProvider for Ollama local models
type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(cfg *config.Config) (*OllamaProvider, error) {
	baseURL := cfg.LLMProvider.ApiKey // Using APIKey field as base URL for Ollama
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	return &OllamaProvider{
		baseURL: baseURL,
		model:   cfg.LLMProvider.Model,
		client:  &http.Client{},
	}, nil
}

// Chat sends a chat request to Ollama
func (p *OllamaProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "llama2"
	}

	// Build request
	payload := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
		"stream":   false,
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse response
	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response error: %w", err)
	}

	return &ChatResponse{
		Content: result.Message.Content,
	}, nil
}

// ChatStream sends a chat request and streams the response
func (p *OllamaProvider) ChatStream(ctx context.Context, req *ChatRequest, handler StreamHandler) error {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "llama2"
	}

	payload := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
		"stream":   true,
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		handler.OnError(err)
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		handler.OnError(err)
		return err
	}
	defer resp.Body.Close()

	// Read streaming response line by line
	var content string
	decoder := json.NewDecoder(resp.Body)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var result struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}

		if err := decoder.Decode(&result); err != nil {
			if err == io.EOF {
				break
			}
			handler.OnError(err)
			return err
		}

		content += result.Message.Content
		handler.OnContent(result.Message.Content)

		if result.Done {
			break
		}
	}

	handler.OnComplete(&ChatResponse{
		Content: content,
	})

	return nil
}

// GetName returns the provider name
func (p *OllamaProvider) GetName() string {
	return "ollama"
}
