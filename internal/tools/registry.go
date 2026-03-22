package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]InvokeTool
	metadata map[string]*ToolInfo
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:    make(map[string]InvokeTool),
		metadata: make(map[string]*ToolInfo),
	}
}

func (r *ToolRegistry) Register(tool InvokeTool) error {
	ctx := context.Background()
	info, err := tool.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tool info: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[info.Name]; exists {
		return fmt.Errorf("tool %s already registered", info.Name)
	}

	r.tools[info.Name] = tool
	r.metadata[info.Name] = info
	return nil
}

func (r *ToolRegistry) Get(name string) (InvokeTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) GetInfo(name string) (*ToolInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.metadata[name]
	return info, ok
}

func (r *ToolRegistry) List() []*ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]*ToolInfo, 0, len(r.metadata))
	for _, info := range r.metadata {
		infos = append(infos, info)
	}
	return infos
}

func (r *ToolRegistry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

func (r *ToolRegistry) Invoke(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %s not found", name)
	}

	return tool.Invoke(ctx, params)
}

type DefaultToolRegistry struct {
	*ToolRegistry
}

func NewDefaultToolRegistry(baseDir string) *DefaultToolRegistry {
	r := NewToolRegistry()

	r.Register(&ExecTool{})
	r.Register(NewReadFileTool(baseDir))
	r.Register(NewWriteFileTool(baseDir))
	r.Register(NewListDirTool(baseDir))
	r.Register(NewTodoTool(baseDir + "/.todo.json"))
	r.Register(NewGetCurrentTimeTool())

	return &DefaultToolRegistry{ToolRegistry: r}
}
