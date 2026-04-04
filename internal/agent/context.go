package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"go-claw/internal/skills"
)

type WorkspaceFile string

const (
	FileSOUL      WorkspaceFile = "SOUL.md"
	FileUSER      WorkspaceFile = "USER.md"
	FileMEMORY    WorkspaceFile = "MEMORY.md"
	FileIDENTITY  WorkspaceFile = "IDENTITY.md"
	FileAGENTS    WorkspaceFile = "AGENTS.md"
	FileBOOT      WorkspaceFile = "BOOT.md"
	FileHEARTBEAT WorkspaceFile = "HEARTBEAT.md"
	FileSKILLS    WorkspaceFile = "SKILLS.md"
)

type ContextManager struct {
	workspace    string
	files        map[WorkspaceFile]string
	skillManager *skills.Manager
}

type ContextData struct {
	Identity  string
	Soul      string
	User      string
	Memory    string
	Agents    string
	Boot      string
	Heartbeat string
	Skills    string
	Time      string
	Date      string
	Custom    map[string]string
	Env       *EnvironmentInfo
}

type EnvironmentInfo struct {
	OS          string
	Arch        string
	WorkDir     string
	CurrentTime string
	Date        string
	Timezone    string
	Hostname    string
	Username    string
	ShellHint   string
}

type PromptTemplate struct {
	Context *ContextData
	System  string
	History string
	Input   string
}

func NewContextManager(workspace string) *ContextManager {
	return &ContextManager{
		workspace: workspace,
		files:     make(map[WorkspaceFile]string),
	}
}

func NewContextManagerWithSkills(workspace string, skillManager *skills.Manager) *ContextManager {
	return &ContextManager{
		workspace:    workspace,
		files:        make(map[WorkspaceFile]string),
		skillManager: skillManager,
	}
}

func (cm *ContextManager) SetSkillManager(sm *skills.Manager) {
	cm.skillManager = sm
}

func (cm *ContextManager) Load() error {
	files := []WorkspaceFile{FileSOUL, FileUSER, FileMEMORY, FileIDENTITY, FileAGENTS, FileBOOT, FileHEARTBEAT, FileSKILLS}
	for _, f := range files {
		path := filepath.Join(cm.workspace, string(f))
		if content, err := os.ReadFile(path); err == nil {
			cm.files[f] = string(content)
		}
	}
	return nil
}

func (cm *ContextManager) Get(f WorkspaceFile) string {
	return cm.files[f]
}

func (cm *ContextManager) Set(f WorkspaceFile, content string) error {
	cm.files[f] = content
	path := filepath.Join(cm.workspace, string(f))
	return os.WriteFile(path, []byte(content), 0644)
}

func (cm *ContextManager) GetEnvironmentInfo() *EnvironmentInfo {
	now := time.Now()
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	_, offset := now.Zone()
	tzName := now.Location().String()
	if tzName == "Local" {
		tzName = fmt.Sprintf("UTC%+d", offset/3600)
	}

	var shellHint string
	switch runtime.GOOS {
	case "windows":
		shellHint = `IMPORTANT: You are running on Windows. When using command-line tools:
- Use PowerShell/CMD commands (e.g., dir, type, copy, del, move)
- Use backslashes \ for paths
- Use "cmd /C" for shell commands
- Do NOT use Unix commands like ls, cat, rm, cp, mv`
	case "darwin":
		shellHint = `IMPORTANT: You are running on macOS. When using command-line tools:
- Use Unix/BSD commands (e.g., ls, cat, rm, cp, mv)
- Use forward slashes / for paths
- Use "sh -c" for shell commands
- macOS-specific: use "open" to open files, "brew" for package management`
	default:
		shellHint = `IMPORTANT: You are running on Linux/Unix. When using command-line tools:
- Use Unix/Linux commands (e.g., ls, cat, rm, cp, mv)
- Use forward slashes / for paths
- Use "sh -c" for shell commands`
	}

	return &EnvironmentInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		WorkDir:     cm.workspace,
		CurrentTime: now.Format("15:04:05"),
		Date:        now.Format("2006-01-02"),
		Timezone:    tzName,
		Hostname:    hostname,
		Username:    username,
		ShellHint:   shellHint,
	}
}

func (cm *ContextManager) BuildSystemPrompt() string {
	var sb strings.Builder

	if identity := cm.Get(FileIDENTITY); identity != "" {
		sb.WriteString("## Identity\n")
		sb.WriteString(identity)
		sb.WriteString("\n\n")
	}

	if soul := cm.Get(FileSOUL); soul != "" {
		sb.WriteString("## Soul\n")
		sb.WriteString(soul)
		sb.WriteString("\n\n")
	}

	if user := cm.Get(FileUSER); user != "" {
		sb.WriteString("## User Information\n")
		sb.WriteString(user)
		sb.WriteString("\n\n")
	}

	if memory := cm.Get(FileMEMORY); memory != "" {
		sb.WriteString("## Memory\n")
		sb.WriteString(memory)
		sb.WriteString("\n\n")
	}

	if agents := cm.Get(FileAGENTS); agents != "" {
		sb.WriteString("## Agent Routing\n")
		sb.WriteString(agents)
		sb.WriteString("\n\n")
	}

	if boot := cm.Get(FileBOOT); boot != "" {
		sb.WriteString("## Boot Instructions\n")
		sb.WriteString(boot)
		sb.WriteString("\n\n")
	}

	if heartbeat := cm.Get(FileHEARTBEAT); heartbeat != "" {
		sb.WriteString("## Daily Checklist\n")
		sb.WriteString(heartbeat)
		sb.WriteString("\n\n")
	}

	if skillsPrompt := cm.BuildSkillsPrompt(); skillsPrompt != "" {
		sb.WriteString(skillsPrompt)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (cm *ContextManager) BuildSkillsPrompt() string {
	if cm.skillManager == nil {
		return cm.Get(FileSKILLS)
	}

	skillList := cm.skillManager.ListEnabled()
	if len(skillList) == 0 {
		return cm.Get(FileSKILLS)
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("You have access to the following skills. Each skill provides specialized capabilities.\n\n")
	sb.WriteString("<available_skills>\n")

	for _, skill := range skillList {
		sb.WriteString(fmt.Sprintf("<skill>\n"))
		sb.WriteString(fmt.Sprintf("<name>%s</name>\n", skill.Name))
		sb.WriteString(fmt.Sprintf("<command>%s</command>\n", skill.Command))
		sb.WriteString(fmt.Sprintf("<description>%s</description>\n", skill.Description))
		if skill.Source != "" {
			sb.WriteString(fmt.Sprintf("<location>%s/SKILL.md</location>\n", skill.Source))
		}
		sb.WriteString(fmt.Sprintf("</skill>\n"))
	}

	sb.WriteString("</available_skills>\n\n")
	sb.WriteString("**How to use skills:**\n")
	sb.WriteString("1. When the user's request matches a skill's description, use the `read` tool to load the SKILL.md file from the skill's location\n")
	sb.WriteString("2. Read and follow the instructions in the SKILL.md file\n")
	sb.WriteString("3. Execute the skill's workflow step by step\n")
	sb.WriteString("4. You can also directly invoke a skill by mentioning its command (e.g., `/天气`)\n\n")
	sb.WriteString("**Important:** Do NOT load all skills at once. Only load the skill that is relevant to the current task.\n")

	return sb.String()
}

func (cm *ContextManager) BuildPrompt(req *ExecuteRequest, history string, extraSystem string) string {
	skillsPrompt := ""
	if cm.skillManager != nil {
		skillsPrompt = cm.BuildSkillsPrompt()
	}

	envInfo := cm.GetEnvironmentInfo()

	data := &PromptTemplate{
		Context: &ContextData{
			Soul:      cm.Get(FileSOUL),
			User:      cm.Get(FileUSER),
			Memory:    cm.Get(FileMEMORY),
			Identity:  cm.Get(FileIDENTITY),
			Agents:    cm.Get(FileAGENTS),
			Boot:      cm.Get(FileBOOT),
			Heartbeat: cm.Get(FileHEARTBEAT),
			Skills:    skillsPrompt,
			Env:       envInfo,
		},
		System:  extraSystem,
		History: history,
		Input:   req.Input,
	}

	tmpl, err := template.New("prompt").Parse(`{{if .Context.Identity}}## Identity
{{.Context.Identity}}

{{end}}{{if .Context.Soul}}## Soul
{{.Context.Soul}}

{{end}}{{if .Context.User}}## User
{{.Context.User}}

{{end}}{{if .Context.Memory}}## Memory
{{.Context.Memory}}

{{end}}{{if .Context.Agents}}## Agents
{{.Context.Agents}}

{{end}}{{if .Context.Boot}}## Boot
{{.Context.Boot}}

{{end}}{{if .Context.Heartbeat}}## Heartbeat
{{.Context.Heartbeat}}

{{end}}{{if .Context.Skills}}{{.Context.Skills}}

{{end}}## Environment
- OS: {{.Context.Env.OS}} ({{.Context.Env.Arch}})
- Working Directory: {{.Context.Env.WorkDir}}
- Date: {{.Context.Env.Date}}
- Time: {{.Context.Env.CurrentTime}} ({{.Context.Env.Timezone}})
{{if .Context.Env.Hostname}}- Hostname: {{.Context.Env.Hostname}}{{end}}
{{if .Context.Env.Username}}- User: {{.Context.Env.Username}}{{end}}

{{.Context.Env.ShellHint}}

{{if .System}}{{.System}}

{{end}}## Conversation
{{.History}}

{{if .Input}}## Current Input
{{.Input}}{{end}}`)
	if err != nil {
		return cm.BuildSystemPrompt() + "\n\n" + history + "\n\n" + req.Input
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return cm.BuildSystemPrompt() + "\n\n" + history + "\n\n" + req.Input
	}

	return buf.String()
}

func (cm *ContextManager) AppendMemory(content string) error {
	existing := cm.Get(FileMEMORY)
	if existing != "" {
		existing += "\n"
	}
	existing += "- " + content
	return cm.Set(FileMEMORY, existing)
}

func (cm *ContextManager) UpdateMemory(newMemory string) error {
	return cm.Set(FileMEMORY, newMemory)
}

func (cm *ContextManager) ListFiles() []string {
	files := []string{}
	for _, f := range []WorkspaceFile{FileSOUL, FileUSER, FileMEMORY, FileIDENTITY, FileAGENTS, FileBOOT, FileHEARTBEAT} {
		if cm.Get(f) != "" {
			files = append(files, string(f))
		}
	}
	return files
}

func EnsureWorkspace(workspace string) error {
	files := []WorkspaceFile{FileSOUL, FileUSER, FileMEMORY, FileIDENTITY, FileAGENTS, FileBOOT, FileHEARTBEAT}
	defaults := map[WorkspaceFile]string{
		FileIDENTITY:  "# Identity\n\nYour name is {{.Name}}.\n",
		FileSOUL:      "# Soul\n\nYou are a helpful AI assistant.\n",
		FileUSER:      "# User\n\n",
		FileMEMORY:    "# Memory\n\n",
		FileAGENTS:    "# Agents\n\n",
		FileBOOT:      "# Boot\n\n",
		FileHEARTBEAT: "# Heartbeat\n\n- [ ] Check daily tasks\n",
	}

	for _, f := range files {
		path := filepath.Join(workspace, string(f))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if defaultContent, ok := defaults[f]; ok {
				if err := os.MkdirAll(workspace, 0755); err != nil {
					return fmt.Errorf("failed to create workspace: %w", err)
				}
				if err := os.WriteFile(path, []byte(defaultContent), 0644); err != nil {
					return fmt.Errorf("failed to create %s: %w", f, err)
				}
			}
		}
	}
	return nil
}
