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
	Parameters  ToolParameters `json:"parameters,omitempty"`
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
	Type       ParameterType            `json:"type,omitempty"`
	Properties map[string]ToolParameter `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

type ToolParameter struct {
	Type        ParameterType `json:"type,omitempty"`
	Description string        `json:"description,omitempty"`
	Default     any           `json:"default,omitempty"`
	Enum        []any         `json:"enum,omitempty"`
}
