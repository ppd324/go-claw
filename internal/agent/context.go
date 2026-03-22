package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
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
)

type ContextManager struct {
	workspace string
	files     map[WorkspaceFile]string
}

type ContextData struct {
	Identity  string
	Soul      string
	User      string
	Memory    string
	Agents    string
	Boot      string
	Heartbeat string
	Time      string
	Date      string
	Custom    map[string]string
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

func (cm *ContextManager) Load() error {
	files := []WorkspaceFile{FileSOUL, FileUSER, FileMEMORY, FileIDENTITY, FileAGENTS, FileBOOT, FileHEARTBEAT}
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
	fmt.Println(sb.String())

	return sb.String()
}

func (cm *ContextManager) BuildPrompt(req *ExecuteRequest, history string, extraSystem string) string {
	data := &PromptTemplate{
		Context: &ContextData{
			Soul:      cm.Get(FileSOUL),
			User:      cm.Get(FileUSER),
			Memory:    cm.Get(FileMEMORY),
			Identity:  cm.Get(FileIDENTITY),
			Agents:    cm.Get(FileAGENTS),
			Boot:      cm.Get(FileBOOT),
			Heartbeat: cm.Get(FileHEARTBEAT),
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

{{end}}{{if .System}}{{.System}}

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
