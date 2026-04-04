package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go-claw/internal/skills"
)

type CreateSkillTool struct {
	manager *skills.Manager
}

func NewCreateSkillTool(manager *skills.Manager) *CreateSkillTool {
	return &CreateSkillTool{manager: manager}
}

func (t *CreateSkillTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "create_skill",
		Description: "Create a new skill that defines a reusable workflow or capability. Skills are triggered by slash commands and contain instructions for the AI to follow.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"name": {
					Type:        String,
					Description: "The name of the skill (e.g., 'deploy', 'review-pr', 'summarize')",
				},
				"command": {
					Type:        String,
					Description: "The slash command to trigger the skill (e.g., '/deploy', '/review'). If not provided, will be auto-generated from name",
				},
				"description": {
					Type:        String,
					Description: "A brief description of what this skill does",
				},
				"instructions": {
					Type:        String,
					Description: "Detailed instructions for the AI to follow when this skill is triggered. Be specific about the steps and expected output",
				},
				"tools": {
					Type:        Array,
					Description: "List of tool names that this skill requires (e.g., ['execute_command', 'read_file', 'write_file'])",
				},
				"category": {
					Type:        String,
					Description: "Category for organizing skills (e.g., 'deployment', 'code-review', 'documentation')",
				},
				"tags": {
					Type:        Array,
					Description: "Tags for filtering and searching skills",
				},
				"examples": {
					Type:        Array,
					Description: "Example usages of this skill",
				},
			},
			Required: []string{"name", "description", "instructions"},
		},
	}, nil
}

func (t *CreateSkillTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		Name         string   `json:"name"`
		Command      string   `json:"command"`
		Description  string   `json:"description"`
		Instructions string   `json:"instructions"`
		Tools        []string `json:"tools"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
		Examples     []string `json:"examples"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if p.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if p.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	if p.Instructions == "" {
		return nil, fmt.Errorf("instructions is required")
	}

	command := p.Command
	if command == "" {
		command = "/" + strings.ToLower(strings.ReplaceAll(p.Name, " ", "-"))
	}

	skill, err := t.manager.Create(p.Name, command, p.Description, p.Instructions)
	if err != nil {
		return nil, err
	}

	if len(p.Tools) > 0 {
		skill.Tools = p.Tools
	}
	if p.Category != "" {
		skill.Category = p.Category
	}
	if len(p.Tags) > 0 {
		skill.Tags = p.Tags
	}
	if len(p.Examples) > 0 {
		skill.Examples = p.Examples
	}

	if err := t.manager.Update(skill); err != nil {
		return nil, err
	}

	return &ToolResult{
		Text: fmt.Sprintf("Successfully created skill '%s' with command '%s'\n\nDescription: %s\n\nThe skill is now available. Users can trigger it by typing: %s",
			skill.Name, skill.Command, skill.Description, skill.Command),
	}, nil
}

type ListSkillsTool struct {
	manager *skills.Manager
}

func NewListSkillsTool(manager *skills.Manager) *ListSkillsTool {
	return &ListSkillsTool{manager: manager}
}

func (t *ListSkillsTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "list_skills",
		Description: "List all available skills with their commands and descriptions",
		Parameters: ToolParameters{
			Type:       Object,
			Properties: map[string]ToolParameter{},
		},
	}, nil
}

func (t *ListSkillsTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	skillList := t.manager.ListEnabled()

	if len(skillList) == 0 {
		return &ToolResult{Text: "No skills available. Use create_skill to create a new skill."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Available Skills:\n\n")

	for _, skill := range skillList {
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", skill.Name, skill.Command))
		sb.WriteString(fmt.Sprintf("  %s\n", skill.Description))
		if skill.Category != "" {
			sb.WriteString(fmt.Sprintf("  Category: %s\n", skill.Category))
		}
		sb.WriteString("\n")
	}

	return &ToolResult{Text: sb.String()}, nil
}

type GetSkillTool struct {
	manager *skills.Manager
}

func NewGetSkillTool(manager *skills.Manager) *GetSkillTool {
	return &GetSkillTool{manager: manager}
}

func (t *GetSkillTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "get_skill",
		Description: "Get detailed information about a specific skill",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"command": {
					Type:        String,
					Description: "The command of the skill to retrieve (e.g., '/deploy')",
				},
			},
			Required: []string{"command"},
		},
	}, nil
}

func (t *GetSkillTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		Command string `json:"command"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	skill, ok := t.manager.Get(p.Command)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", p.Command)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", skill.Name))
	sb.WriteString(fmt.Sprintf("**Command:** %s\n\n", skill.Command))
	sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", skill.Description))

	if skill.Version != "" {
		sb.WriteString(fmt.Sprintf("**Version:** %s\n\n", skill.Version))
	}
	if skill.Author != "" {
		sb.WriteString(fmt.Sprintf("**Author:** %s\n\n", skill.Author))
	}
	if skill.Category != "" {
		sb.WriteString(fmt.Sprintf("**Category:** %s\n\n", skill.Category))
	}
	if len(skill.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("**Tags:** %s\n\n", strings.Join(skill.Tags, ", ")))
	}
	if len(skill.Tools) > 0 {
		sb.WriteString("**Required Tools:**\n")
		for _, tool := range skill.Tools {
			sb.WriteString(fmt.Sprintf("- %s\n", tool))
		}
		sb.WriteString("\n")
	}
	if len(skill.Examples) > 0 {
		sb.WriteString("**Examples:**\n")
		for _, example := range skill.Examples {
			sb.WriteString(fmt.Sprintf("- %s\n", example))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**Instructions:**\n")
	sb.WriteString(skill.Instructions)

	return &ToolResult{Text: sb.String()}, nil
}

type UpdateSkillTool struct {
	manager *skills.Manager
}

func NewUpdateSkillTool(manager *skills.Manager) *UpdateSkillTool {
	return &UpdateSkillTool{manager: manager}
}

func (t *UpdateSkillTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "update_skill",
		Description: "Update an existing skill's properties",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"command": {
					Type:        String,
					Description: "The command of the skill to update",
				},
				"description": {
					Type:        String,
					Description: "New description for the skill",
				},
				"instructions": {
					Type:        String,
					Description: "New instructions for the skill",
				},
				"tools": {
					Type:        Array,
					Description: "New list of required tools",
				},
				"category": {
					Type:        String,
					Description: "New category for the skill",
				},
				"tags": {
					Type:        Array,
					Description: "New tags for the skill",
				},
				"examples": {
					Type:        Array,
					Description: "New examples for the skill",
				},
			},
			Required: []string{"command"},
		},
	}, nil
}

func (t *UpdateSkillTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		Command      string   `json:"command"`
		Description  string   `json:"description"`
		Instructions string   `json:"instructions"`
		Tools        []string `json:"tools"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
		Examples     []string `json:"examples"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	skill, ok := t.manager.Get(p.Command)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", p.Command)
	}

	if p.Description != "" {
		skill.Description = p.Description
	}
	if p.Instructions != "" {
		skill.Instructions = p.Instructions
	}
	if p.Tools != nil {
		skill.Tools = p.Tools
	}
	if p.Category != "" {
		skill.Category = p.Category
	}
	if p.Tags != nil {
		skill.Tags = p.Tags
	}
	if p.Examples != nil {
		skill.Examples = p.Examples
	}

	if err := t.manager.Update(skill); err != nil {
		return nil, err
	}

	return &ToolResult{
		Text: fmt.Sprintf("Successfully updated skill '%s'", skill.Name),
	}, nil
}

type DeleteSkillTool struct {
	manager *skills.Manager
}

func NewDeleteSkillTool(manager *skills.Manager) *DeleteSkillTool {
	return &DeleteSkillTool{manager: manager}
}

func (t *DeleteSkillTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "delete_skill",
		Description: "Delete a skill permanently",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"command": {
					Type:        String,
					Description: "The command of the skill to delete",
				},
			},
			Required: []string{"command"},
		},
	}, nil
}

func (t *DeleteSkillTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		Command string `json:"command"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if err := t.manager.Delete(p.Command); err != nil {
		return nil, err
	}

	return &ToolResult{
		Text: fmt.Sprintf("Successfully deleted skill with command '%s'", p.Command),
	}, nil
}
