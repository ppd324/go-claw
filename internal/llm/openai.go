package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go-claw/internal/config"
)

type OpenAIProvider struct {
	client *http.Client
	apiKey string
	model  string
}

func NewOpenAIProvider(cfg *config.Config) (*OpenAIProvider, error) {
	apiKey := cfg.LLMProvider.ApiKey
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key required")
	}

	return &OpenAIProvider{
		client: &http.Client{Timeout: cfg.LLMProvider.Timeout},
		apiKey: apiKey,
		model:  cfg.LLMProvider.Model,
	}, nil
}

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gpt-4"
	}

	payload := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		payload["tools"] = tools
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Choices []struct {
			Message struct {
				Content    string `json:"content"`
				ToolCalls  []struct {
					Index    int `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	toolCalls := make([]ToolCall, 0, len(result.Choices[0].Message.ToolCalls))
	for _, tc := range result.Choices[0].Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			Index:  tc.Index,
			ID:     tc.ID,
			Type:   tc.Type,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return &ChatResponse{
		Content:      result.Choices[0].Message.Content,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
		StopReason:   result.Choices[0].FinishReason,
		ToolCalls:    toolCalls,
	}, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *ChatRequest, handler StreamHandler) error {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gpt-4"
	}

	payload := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
		"stream":   true,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		payload["tools"] = tools
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions", bytes.NewReader(body))
	if err != nil {
		handler.OnError(err)
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	reader := bufio.NewReader(resp.Body)
	var content string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			handler.OnError(err)
			return err
		}

		line = line[:len(line)-1]
		if line == "" || line == "data: [DONE]" {
			continue
		}

		if len(line) < 6 || line[:6] != "data: " {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content += chunk.Choices[0].Delta.Content
			handler.OnContent(chunk.Choices[0].Delta.Content)
		}
	}

	handler.OnComplete(&ChatResponse{
		Content: content,
	})

	return nil
}

func (p *OpenAIProvider) GetName() string {
	return "openai"
}
