package skills

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"
)

type Skill struct {
	Name         string            `json:"name" yaml:"name"`
	Command      string            `json:"command" yaml:"command"`
	Description  string            `json:"description" yaml:"description"`
	Version      string            `json:"version" yaml:"version"`
	Author       string            `json:"author" yaml:"author"`
	Category     string            `json:"category" yaml:"category"`
	Tags         []string          `json:"tags" yaml:"tags"`
	Instructions string            `json:"instructions" yaml:"-"`
	Tools        []string          `json:"tools" yaml:"tools"`
	Examples     []string          `json:"examples" yaml:"examples"`
	Variables    map[string]string `json:"variables" yaml:"variables"`
	BeforeShell  string            `json:"before_shell" yaml:"before_shell"`
	AfterShell   string            `json:"after_shell" yaml:"after_shell"`
	Enabled      bool              `json:"enabled" yaml:"-"`
	Source       string            `json:"source" yaml:"-"`
	LoadedAt     time.Time         `json:"loaded_at" yaml:"-"`
}

type skillFrontmatter struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Description string            `yaml:"description"`
	Version     string            `yaml:"version"`
	Author      string            `yaml:"author"`
	Category    string            `yaml:"category"`
	Tags        []string          `yaml:"tags"`
	Tools       []string          `yaml:"tools"`
	Examples    []string          `yaml:"examples"`
	Variables   map[string]string `yaml:"variables"`
	BeforeShell string            `yaml:"before_shell"`
	AfterShell  string            `yaml:"after_shell"`
}

type Manager struct {
	skillsDir string
	skills    map[string]*Skill
}

func NewManager(skillsDir string) *Manager {
	return &Manager{
		skillsDir: skillsDir,
		skills:    make(map[string]*Skill),
	}
}

func (m *Manager) Load() error {
	if _, err := os.Stat(m.skillsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(m.skillsDir, 0755); err != nil {
			return fmt.Errorf("failed to create skills directory: %w", err)
		}
		return nil
	}

	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		return fmt.Errorf("failed to read skills directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillPath := filepath.Join(m.skillsDir, entry.Name())
			fmt.Println("load sill:", skillPath)
			if err := m.loadSkill(skillPath); err != nil {
				slog.Warn("Failed to load skills", "error", err)
				continue
			}
		}
	}
	fmt.Println(m.skills)

	return nil
}

func (m *Manager) loadSkill(skillPath string) error {
	skillFile := filepath.Join(skillPath, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	skill := parseSkillMD(string(content))
	if skill == nil {
		return fmt.Errorf("failed to parse SKILL.md")
	}

	skill.Source = skillPath
	skill.LoadedAt = time.Now()
	skill.Enabled = true

	m.skills[skill.Command] = skill
	return nil
}

func parseSkillMD(content string) *Skill {
	skill := &Skill{
		Tools:     []string{},
		Examples:  []string{},
		Tags:      []string{},
		Variables: make(map[string]string),
	}

	frontmatter, body := extractFrontmatter(content)
	if frontmatter != nil {
		skill.Name = frontmatter.Name
		skill.Command = frontmatter.Command
		skill.Description = frontmatter.Description
		skill.Version = frontmatter.Version
		skill.Author = frontmatter.Author
		skill.Category = frontmatter.Category
		skill.Tags = frontmatter.Tags
		skill.Tools = frontmatter.Tools
		skill.Examples = frontmatter.Examples
		skill.Variables = frontmatter.Variables
		skill.BeforeShell = frontmatter.BeforeShell
		skill.AfterShell = frontmatter.AfterShell
		skill.Instructions = strings.TrimSpace(body)
	} else {
		sections := parseSections(content)

		if name, ok := sections["name"]; ok {
			skill.Name = strings.TrimSpace(name)
		}
		if cmd, ok := sections["command"]; ok {
			skill.Command = strings.TrimSpace(cmd)
		} else if name, ok := sections["name"]; ok {
			skill.Command = "/" + strings.ToLower(strings.TrimSpace(name))
		}
		if desc, ok := sections["description"]; ok {
			skill.Description = strings.TrimSpace(desc)
		}
		if version, ok := sections["version"]; ok {
			skill.Version = strings.TrimSpace(version)
		}
		if author, ok := sections["author"]; ok {
			skill.Author = strings.TrimSpace(author)
		}
		if category, ok := sections["category"]; ok {
			skill.Category = strings.TrimSpace(category)
		}
		if tags, ok := sections["tags"]; ok {
			for _, tag := range strings.Split(tags, ",") {
				t := strings.TrimSpace(tag)
				if t != "" {
					skill.Tags = append(skill.Tags, t)
				}
			}
		}
		if tools, ok := sections["tools"]; ok {
			for _, tool := range strings.Split(tools, ",") {
				t := strings.TrimSpace(tool)
				if t != "" {
					skill.Tools = append(skill.Tools, t)
				}
			}
		}
		if examples, ok := sections["examples"]; ok {
			skill.Examples = parseList(examples)
		}
		if before, ok := sections["before_shell"]; ok {
			skill.BeforeShell = strings.TrimSpace(before)
		}
		if after, ok := sections["after_shell"]; ok {
			skill.AfterShell = strings.TrimSpace(after)
		}
		if variables, ok := sections["variables"]; ok {
			skill.Variables = parseVariables(variables)
		}
		if instructions, ok := sections["instructions"]; ok {
			skill.Instructions = strings.TrimSpace(instructions)
		} else {
			skill.Instructions = extractInstructions(content)
		}
	}

	if skill.Name == "" || skill.Command == "" {
		return nil
	}

	return skill
}

func extractFrontmatter(content string) (*skillFrontmatter, string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, ""
	}

	var endIndex int
	if strings.HasPrefix(content, "---\r\n") {
		endIndex = strings.Index(content[5:], "\r\n---")
		if endIndex == -1 {
			return nil, ""
		}
		endIndex += 5
	} else {
		endIndex = strings.Index(content[4:], "\n---")
		if endIndex == -1 {
			return nil, ""
		}
		endIndex += 4
	}

	var yamlContent string
	var body string
	if strings.HasPrefix(content, "---\r\n") {
		yamlContent = content[5:endIndex]
		body = content[endIndex+5:]
	} else {
		yamlContent = content[4:endIndex]
		body = content[endIndex+4:]
	}

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, ""
	}

	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "---")
	body = strings.TrimSpace(body)

	return &fm, body
}

func parseSections(content string) map[string]string {
	sections := make(map[string]string)
	re := regexp.MustCompile(`(?i)^##\s*([a-zA-Z_]+)`)

	lines := strings.Split(content, "\n")
	var currentKey string
	var currentValue strings.Builder

	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) >= 2 {
			if currentKey != "" {
				sections[currentKey] = strings.TrimSpace(currentValue.String())
			}
			currentKey = strings.ToLower(strings.TrimSpace(matches[1]))
			currentValue.Reset()
		} else if currentKey != "" {
			if currentValue.Len() > 0 {
				currentValue.WriteString("\n")
			}
			currentValue.WriteString(line)
		}
	}

	if currentKey != "" {
		sections[currentKey] = strings.TrimSpace(currentValue.String())
	}

	return sections
}

func parseList(content string) []string {
	var items []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			items = append(items, strings.TrimPrefix(line, "- "))
			items[len(items)-1] = strings.TrimPrefix(items[len(items)-1], "* ")
		}
	}
	return items
}

func parseVariables(content string) map[string]string {
	vars := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			parts := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "), ":", 2)
			if len(parts) == 2 {
				vars[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	return vars
}

func extractInstructions(content string) string {
	sections := parseSections(content)
	if instr, ok := sections["instructions"]; ok {
		return instr
	}
	return ""
}

func (m *Manager) Get(command string) (*Skill, bool) {
	skill, ok := m.skills[command]
	return skill, ok
}

func (m *Manager) GetByName(name string) (*Skill, bool) {
	for _, skill := range m.skills {
		if strings.EqualFold(skill.Name, name) {
			return skill, true
		}
	}
	return nil, false
}

func (m *Manager) List() []*Skill {
	skills := make([]*Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		skills = append(skills, skill)
	}
	return skills
}

func (m *Manager) ListEnabled() []*Skill {
	skills := make([]*Skill, 0)
	for _, skill := range m.skills {
		if skill.Enabled {
			skills = append(skills, skill)
		}
	}
	return skills
}

func (m *Manager) Create(name, command, description, instructions string) (*Skill, error) {
	skillDir := filepath.Join(m.skillsDir, name)
	if _, err := os.Stat(skillDir); err == nil {
		return nil, fmt.Errorf("skill '%s' already exists", name)
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skill directory: %w", err)
	}

	skill := &Skill{
		Name:         name,
		Command:      command,
		Description:  description,
		Instructions: instructions,
		Version:      "1.0.0",
		Enabled:      true,
		Source:       skillDir,
		LoadedAt:     time.Now(),
		Tools:        []string{},
		Examples:     []string{},
		Tags:         []string{},
		Variables:    make(map[string]string),
	}

	if !strings.HasPrefix(skill.Command, "/") {
		skill.Command = "/" + skill.Command
	}

	content := skill.ToMarkdown()
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		os.RemoveAll(skillDir)
		return nil, fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	m.skills[skill.Command] = skill
	return skill, nil
}

func (m *Manager) Update(skill *Skill) error {
	if skill.Source == "" {
		return fmt.Errorf("skill has no source path")
	}

	content := skill.ToMarkdown()
	skillFile := filepath.Join(skill.Source, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	m.skills[skill.Command] = skill
	return nil
}

func (m *Manager) Delete(command string) error {
	skill, ok := m.skills[command]
	if !ok {
		return fmt.Errorf("skill not found: %s", command)
	}

	if skill.Source != "" {
		if err := os.RemoveAll(skill.Source); err != nil {
			return fmt.Errorf("failed to remove skill directory: %w", err)
		}
	}

	delete(m.skills, command)
	return nil
}

func (s *Skill) ToMarkdown() string {
	var sb strings.Builder

	fm := skillFrontmatter{
		Name:        s.Name,
		Command:     s.Command,
		Description: s.Description,
		Version:     s.Version,
		Author:      s.Author,
		Category:    s.Category,
		Tags:        s.Tags,
		Tools:       s.Tools,
		Examples:    s.Examples,
		Variables:   s.Variables,
		BeforeShell: s.BeforeShell,
		AfterShell:  s.AfterShell,
	}

	yamlData, err := yaml.Marshal(&fm)
	if err != nil {
		return s.toMarkdownLegacy()
	}

	sb.WriteString("---\n")
	sb.WriteString(string(yamlData))
	sb.WriteString("---\n\n")

	if s.Instructions != "" {
		sb.WriteString(s.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (s *Skill) toMarkdownLegacy() string {
	var sb strings.Builder

	sb.WriteString("# ")
	sb.WriteString(s.Name)
	sb.WriteString("\n\n")

	if s.Description != "" {
		sb.WriteString("## Description\n")
		sb.WriteString(s.Description)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Command\n")
	sb.WriteString(s.Command)
	sb.WriteString("\n\n")

	if s.Version != "" {
		sb.WriteString("## Version\n")
		sb.WriteString(s.Version)
		sb.WriteString("\n\n")
	}

	if s.Author != "" {
		sb.WriteString("## Author\n")
		sb.WriteString(s.Author)
		sb.WriteString("\n\n")
	}

	if s.Category != "" {
		sb.WriteString("## Category\n")
		sb.WriteString(s.Category)
		sb.WriteString("\n\n")
	}

	if len(s.Tags) > 0 {
		sb.WriteString("## Tags\n")
		for _, tag := range s.Tags {
			sb.WriteString("- ")
			sb.WriteString(tag)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(s.Tools) > 0 {
		sb.WriteString("## Tools\n")
		for _, tool := range s.Tools {
			sb.WriteString("- ")
			sb.WriteString(tool)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(s.Variables) > 0 {
		sb.WriteString("## Variables\n")
		for k, v := range s.Variables {
			sb.WriteString("- ")
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(s.Examples) > 0 {
		sb.WriteString("## Examples\n")
		for _, example := range s.Examples {
			sb.WriteString("- ")
			sb.WriteString(example)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if s.BeforeShell != "" {
		sb.WriteString("## Before_Shell\n")
		sb.WriteString(s.BeforeShell)
		sb.WriteString("\n\n")
	}

	if s.AfterShell != "" {
		sb.WriteString("## After_Shell\n")
		sb.WriteString(s.AfterShell)
		sb.WriteString("\n\n")
	}

	if s.Instructions != "" {
		sb.WriteString("## Instructions\n")
		sb.WriteString(s.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (s *Skill) RenderInstructions(context map[string]interface{}) (string, error) {
	if len(s.Variables) == 0 && len(context) == 0 {
		return s.Instructions, nil
	}

	tmpl, err := template.New("instructions").Parse(s.Instructions)
	if err != nil {
		return s.Instructions, nil
	}

	data := make(map[string]interface{})
	for k, v := range s.Variables {
		data[k] = v
	}
	for k, v := range context {
		data[k] = v
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return s.Instructions, nil
	}

	return buf.String(), nil
}

func (s *Skill) BuildSystemPrompt() string {
	var sb strings.Builder

	sb.WriteString("## Skill: ")
	sb.WriteString(s.Name)
	sb.WriteString("\n\n")

	if s.Description != "" {
		sb.WriteString("**Description:** ")
		sb.WriteString(s.Description)
		sb.WriteString("\n\n")
	}

	sb.WriteString("**Trigger:** Use the command `")
	sb.WriteString(s.Command)
	sb.WriteString("` to activate this skill.\n\n")

	if s.Instructions != "" {
		sb.WriteString("### Instructions\n")
		sb.WriteString(s.Instructions)
		sb.WriteString("\n\n")
	}

	if len(s.Tools) > 0 {
		sb.WriteString("### Required Tools\n")
		for _, tool := range s.Tools {
			sb.WriteString("- ")
			sb.WriteString(tool)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(s.Examples) > 0 {
		sb.WriteString("### Examples\n")
		for _, example := range s.Examples {
			sb.WriteString("- ")
			sb.WriteString(example)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
