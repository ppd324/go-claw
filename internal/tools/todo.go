package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type TodoItem struct {
	ID          string     `json:"id"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type TodoTool struct {
	mu     sync.RWMutex
	file   string
	items  []TodoItem
	nextID int
}

func NewTodoTool(file string) *TodoTool {
	tt := &TodoTool{
		file:   file,
		items:  make([]TodoItem, 0),
		nextID: 1,
	}
	tt.load()
	return tt
}

func (t *TodoTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "todo",
		Description: "Manage a todo list for tracking tasks. Supports add, list, complete, and delete operations.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"action": {
					Type:        String,
					Description: "Action to perform: add, list, complete, delete, clear",
					Enum:        []any{"add", "list", "complete", "delete", "clear"},
				},
				"content": {
					Type:        String,
					Description: "Task content (for add action)",
				},
				"id": {
					Type:        String,
					Description: "Task ID (for complete/delete actions)",
				},
				"priority": {
					Type:        Number,
					Description: "Priority level 1-5, higher is more important (default: 3)",
					Default:     3,
				},
				"status": {
					Type:        String,
					Description: "Filter by status: pending, completed, all (for list action)",
					Default:     "pending",
				},
			},
			Required: []string{"action"},
		},
	}, nil
}

type TodoParams struct {
	Action   string `json:"action"`
	Content  string `json:"content"`
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
}

func (t *TodoTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p TodoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch p.Action {
	case "add":
		return t.handleAdd(p.Content, p.Priority)
	case "list":
		return t.handleList(p.Status)
	case "complete":
		return t.handleComplete(p.ID)
	case "delete":
		return t.handleDelete(p.ID)
	case "clear":
		return t.handleClear()
	default:
		return &ToolResult{Text: fmt.Sprintf("unknown action: %s", p.Action)}, nil
	}
}

func (t *TodoTool) handleAdd(content string, priority int) (*ToolResult, error) {
	if content == "" {
		return &ToolResult{Text: "content is required for add action"}, nil
	}
	if priority == 0 {
		priority = 3
	}

	item := TodoItem{
		ID:        fmt.Sprintf("%d", t.nextID),
		Content:   content,
		Status:    "pending",
		Priority:  priority,
		CreatedAt: time.Now(),
	}
	t.nextID++
	t.items = append(t.items, item)
	t.save()

	return &ToolResult{Text: fmt.Sprintf("Added task #%s: %s (priority: %d)", item.ID, item.Content, item.Priority)}, nil
}

func (t *TodoTool) handleList(status string) (*ToolResult, error) {
	if status == "" {
		status = "pending"
	}

	var filtered []TodoItem
	for _, item := range t.items {
		if status == "all" || item.Status == status {
			filtered = append(filtered, item)
		}
	}

	if len(filtered) == 0 {
		return &ToolResult{Text: "No tasks found"}, nil
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Tasks (%s):\n", status))
	builder.WriteString(strings.Repeat("-", 40) + "\n")

	for _, item := range filtered {
		checkbox := "[ ]"
		if item.Status == "completed" {
			checkbox = "[x]"
		}
		builder.WriteString(fmt.Sprintf("%s #%s %s (p%d)\n", checkbox, item.ID, item.Content, item.Priority))
	}

	return &ToolResult{Text: builder.String()}, nil
}

func (t *TodoTool) handleComplete(id string) (*ToolResult, error) {
	if id == "" {
		return &ToolResult{Text: "id is required for complete action"}, nil
	}

	for i, item := range t.items {
		if item.ID == id {
			if t.items[i].Status == "completed" {
				return &ToolResult{Text: fmt.Sprintf("Task #%s is already completed", id)}, nil
			}
			now := time.Now()
			t.items[i].Status = "completed"
			t.items[i].CompletedAt = &now
			t.save()
			return &ToolResult{Text: fmt.Sprintf("Completed task #%s: %s", id, t.items[i].Content)}, nil
		}
	}

	return &ToolResult{Text: fmt.Sprintf("Task #%s not found", id)}, nil
}

func (t *TodoTool) handleDelete(id string) (*ToolResult, error) {
	if id == "" {
		return &ToolResult{Text: "id is required for delete action"}, nil
	}

	for i, item := range t.items {
		if item.ID == id {
			content := t.items[i].Content
			t.items = append(t.items[:i], t.items[i+1:]...)
			t.save()
			return &ToolResult{Text: fmt.Sprintf("Deleted task #%s: %s", id, content)}, nil
		}
	}

	return &ToolResult{Text: fmt.Sprintf("Task #%s not found", id)}, nil
}

func (t *TodoTool) handleClear() (*ToolResult, error) {
	completed := 0
	var pending []TodoItem
	for _, item := range t.items {
		if item.Status == "completed" {
			completed++
		} else {
			pending = append(pending, item)
		}
	}

	t.items = pending
	t.save()

	return &ToolResult{Text: fmt.Sprintf("Cleared %d completed tasks, %d pending remaining", completed, len(t.items))}, nil
}

func (t *TodoTool) load() {
	if t.file == "" {
		return
	}

	data, err := os.ReadFile(t.file)
	if err != nil {
		return
	}

	var items []TodoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}

	t.items = items
	maxID := 0
	for _, item := range items {
		var id int
		fmt.Sscanf(item.ID, "%d", &id)
		if id > maxID {
			maxID = id
		}
	}
	t.nextID = maxID + 1
}

func (t *TodoTool) save() {
	if t.file == "" {
		return
	}

	data, err := json.MarshalIndent(t.items, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(t.file, data, 0644)
}

func (t *TodoTool) GetItems() []TodoItem {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.items
}
