package tools

import (
	"context"
	"encoding/json"
	"time"
)

// GetCurrentTimeTool implements the tool for getting current time
type GetCurrentTimeTool struct{}

// NewGetCurrentTimeTool creates a new get current time tool
func NewGetCurrentTimeTool() *GetCurrentTimeTool {
	return &GetCurrentTimeTool{}
}

// Info returns tool information
func (t *GetCurrentTimeTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "get_current_time",
		Description: "Get the current date and time. Use this when you need to know the current time to schedule reminders or check if a specific time has passed.",
		Parameters: ToolParameters{
			Type:       Object,
			Properties: map[string]ToolParameter{},
		},
	}, nil
}

// Invoke executes the tool
func (t *GetCurrentTimeTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	now := time.Now()

	result := map[string]interface{}{
		"timestamp":       now.Unix(),
		"iso8601":         now.Format(time.RFC3339),
		"datetime":        now.Format("2006-01-02 15:04:05"),
		"date":            now.Format("2006-01-02"),
		"time":            now.Format("15:04:05"),
		"year":            now.Year(),
		"month":           int(now.Month()),
		"day":             now.Day(),
		"hour":            now.Hour(),
		"minute":          now.Minute(),
		"second":          now.Second(),
		"weekday":         now.Weekday().String(),
		"timezone":        now.Location().String(),
		"timezone_offset": now.Unix() - now.Truncate(time.Hour).Unix(),
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		return &ToolResult{Text: "Failed to format time: " + err.Error()}, nil
	}

	return &ToolResult{Text: string(jsonData)}, nil
}
