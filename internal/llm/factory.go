package llm

import (
	"context"
	"fmt"
	"strings"

	arkmodel "github.com/cloudwego/eino-ext/components/model/ark"
	claudemodel "github.com/cloudwego/eino-ext/components/model/claude"
	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"go-claw/internal/config"
)

type AgentModelConfig struct {
	Provider    string
	Model       string
	BaseURL     string
	APIKey      string
	MaxTokens   int
	Temperature float64
	TimeoutMs   int64
}

func BuildToolCallingChatModel(ctx context.Context, cfg *config.Config, agentCfg AgentModelConfig) (einomodel.ToolCallingChatModel, string, string, error) {
	provider := strings.ToLower(strings.TrimSpace(agentCfg.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.LLMProvider.Provider))
	}
	if provider == "" {
		provider = "ark"
	}

	modelName := strings.TrimSpace(agentCfg.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(cfg.LLMProvider.Model)
	}
	if modelName == "" {
		return nil, provider, "", fmt.Errorf("model required")
	}

	baseURL := strings.TrimSpace(agentCfg.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.LLMProvider.BaseUrl)
	}
	apiKey := strings.TrimSpace(agentCfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(cfg.LLMProvider.ApiKey)
	}
	maxTokens := agentCfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.LLMProvider.MaxTokens
	}
	temp := float32(agentCfg.Temperature)
	if temp == 0 && cfg.LLMProvider.Temperature != 0 {
		temp = float32(cfg.LLMProvider.Temperature)
	}

	switch provider {
	case "ark":
		c := &arkmodel.ChatModelConfig{
			APIKey:  apiKey,
			Model:   modelName,
			BaseURL: baseURL,
		}
		if maxTokens > 0 {
			c.MaxTokens = &maxTokens
		}
		if temp != 0 {
			c.Temperature = &temp
		}
		model, err := arkmodel.NewChatModel(ctx, c)
		return model, provider, modelName, err
	case "openai":
		c := &openaimodel.ChatModelConfig{
			APIKey:  apiKey,
			Model:   modelName,
			BaseURL: baseURL,
		}
		if maxTokens > 0 {
			c.MaxCompletionTokens = &maxTokens
		}
		if temp != 0 {
			c.Temperature = &temp
		}
		model, err := openaimodel.NewChatModel(ctx, c)
		return model, provider, modelName, err
	case "claude", "anthropic":
		c := &claudemodel.Config{
			APIKey: apiKey,
			Model:  modelName,
		}
		if baseURL != "" {
			c.BaseURL = &baseURL
		}
		if maxTokens > 0 {
			c.MaxTokens = maxTokens
		}
		if temp != 0 {
			c.Temperature = &temp
		}
		model, err := claudemodel.NewChatModel(ctx, c)
		return model, "claude", modelName, err
	case "ollama":
		c := &ollamamodel.ChatModelConfig{
			BaseURL: baseURL,
			Model:   modelName,
		}
		model, err := ollamamodel.NewChatModel(ctx, c)
		return model, provider, modelName, err
	default:
		return nil, provider, modelName, fmt.Errorf("unsupported llm provider: %s", provider)
	}
}

func ToSchemaMessages(systemPrompt string, messages []Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		result = append(result, schema.SystemMessage(systemPrompt))
	}
	for _, msg := range messages {
		switch msg.Role {
		case string(schema.System):
			result = append(result, schema.SystemMessage(msg.Content))
		case string(schema.Assistant):
			result = append(result, schema.AssistantMessage(msg.Content, nil))
		case string(schema.Tool):
			result = append(result, schema.ToolMessage(msg.Content, msg.Content))
		default:
			result = append(result, schema.UserMessage(msg.Content))
		}
	}
	return result
}

func ExtractText(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	if len(msg.ToolCalls) > 0 {
		return fmt.Sprintf("tool_calls:%d", len(msg.ToolCalls))
	}
	return ""
}
