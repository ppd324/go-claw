package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ReadFileTool struct {
	baseDir string
}

func NewReadFileTool(baseDir string) *ReadFileTool {
	return &ReadFileTool{baseDir: baseDir}
}

func (t *ReadFileTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "read_file",
		Description: "Read the contents of a file. Returns the full file content.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"path": {
					Type:        String,
					Description: "Relative or absolute file path to read",
				},
				"offset": {
					Type:        Number,
					Description: "Line offset to start reading from (0-based)",
					Default:     0,
				},
				"limit": {
					Type:        Number,
					Description: "Maximum number of lines to read (0 = no limit)",
					Default:     0,
				},
			},
			Required: []string{"path"},
		},
	}, nil
}

type ReadFileParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *ReadFileTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p ReadFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	if t.baseDir != "" && !filepath.IsAbs(p.Path) {
		p.Path = filepath.Join(t.baseDir, p.Path)
	}

	content, err := os.ReadFile(p.Path)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to read file: %v", err)}, nil
	}

	lines := strings.Split(string(content), "\n")
	if p.Offset > 0 && p.Offset < len(lines) {
		lines = lines[p.Offset:]
	}
	if p.Limit > 0 && p.Limit < len(lines) {
		lines = lines[:p.Limit]
	}

	return &ToolResult{Text: strings.Join(lines, "\n")}, nil
}

type WriteFileTool struct {
	baseDir string
}

func NewWriteFileTool(baseDir string) *WriteFileTool {
	return &WriteFileTool{baseDir: baseDir}
}

func (t *WriteFileTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "write_file",
		Description: "Create or overwrite a file with content. WARNING: This will overwrite existing files!",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"path": {
					Type:        String,
					Description: "Relative or absolute file path to write",
				},
				"content": {
					Type:        String,
					Description: "Content to write to the file",
				},
				"append": {
					Type:        Boolean,
					Description: "Append to existing file instead of overwriting",
					Default:     false,
				},
			},
			Required: []string{"path", "content"},
		},
	}, nil
}

type WriteFileParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

func (t *WriteFileTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p WriteFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	if t.baseDir != "" && !filepath.IsAbs(p.Path) {
		p.Path = filepath.Join(t.baseDir, p.Path)
	}

	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to create directory: %v", err)}, nil
	}

	var err error
	if p.Append {
		err = os.WriteFile(p.Path, []byte(p.Content), 0644)
		if err == nil {
			err = os.WriteFile(p.Path, []byte("\n"+p.Content), 0644)
		}
	} else {
		err = os.WriteFile(p.Path, []byte(p.Content), 0644)
	}

	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to write file: %v", err)}, nil
	}

	return &ToolResult{Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len(p.Content), p.Path)}, nil
}

type ListDirTool struct {
	baseDir string
}

func NewListDirTool(baseDir string) *ListDirTool {
	return &ListDirTool{baseDir: baseDir}
}

func (t *ListDirTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "list_dir",
		Description: "List files and directories in a given path",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"path": {
					Type:        String,
					Description: "Directory path to list (relative to base or absolute)",
				},
				"recursive": {
					Type:        Boolean,
					Description: "List subdirectories recursively",
					Default:     false,
				},
			},
			Required: []string{"path"},
		},
	}, nil
}

type ListDirParams struct {
	Path     string `json:"path"`
	Recursive bool  `json:"recursive"`
}

func (t *ListDirTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p ListDirParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to parse params: %v", err)}, nil
	}

	if t.baseDir != "" && !filepath.IsAbs(p.Path) {
		p.Path = filepath.Join(t.baseDir, p.Path)
	}

	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("failed to read directory: %v", err)}, nil
	}

	var result strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			result.WriteString("[DIR]  ")
		} else {
			result.WriteString("[FILE] ")
		}
		result.WriteString(entry.Name())
		result.WriteString("\n")
	}

	return &ToolResult{Text: result.String()}, nil
}
