package tools

import (
	"context"
	"encoding/json"
)

type BaseTool interface {
	Info(ctx context.Context) (*ToolInfo, error)
}

type Option struct {
	fn any
}

type InvokeTool interface {
	BaseTool
	Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error)
}

type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

type ToolResult struct {
	Text  string `json:"text"`
	Image string `json:"image"`
}

type ParameterType string

const (
	String  ParameterType = "string"
	Number  ParameterType = "number"
	Boolean ParameterType = "boolean"
	Object  ParameterType = "object"
	Array   ParameterType = "array"
)

type ToolParameters struct {
	Type       ParameterType            `json:"type"`
	Properties map[string]ToolParameter `json:"properties"`
	Required   []string                 `json:"required"`
}

type ToolParameter struct {
	Type        ParameterType `json:"type"`
	Description string        `json:"description"`
	Default     any           `json:"default"`
	Enum        []any         `json:"enum"`
}
