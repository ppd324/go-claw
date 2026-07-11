package agent

import (
	"runtime"
	"strings"
	"testing"
)

func TestBuildSystemPromptIncludesDefaultsAndRuntime(t *testing.T) {
	workspace := t.TempDir()
	cm := NewContextManager(workspace)
	prompt := cm.BuildSystemPrompt()

	checks := []string{
		"## Identity",
		"You are go-claw agent",
		"## Operating Principles",
		"## Runtime Environment",
		"- Current timestamp:",
		"- Timezone:",
		"- Operating system: " + runtime.GOOS,
		"- Architecture: " + runtime.GOARCH,
		"- Working directory: " + workspace,
		"### Shell Guidance",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("system prompt missing %q:\n%s", check, prompt)
		}
	}
}

func TestBuildSystemPromptOmitsEmptyWorkspaceSections(t *testing.T) {
	cm := NewContextManager(t.TempDir())
	cm.files[FileUSER] = "# User\n\n"
	cm.files[FileMEMORY] = "# Memory\n\n"
	cm.files[FileAGENTS] = "# Agents\n\n"
	prompt := cm.BuildSystemPrompt()
	if strings.Contains(prompt, "## User Information") || strings.Contains(prompt, "## Memory") || strings.Contains(prompt, "## Agent Routing") {
		t.Fatalf("empty workspace sections must not be injected:\n%s", prompt)
	}
}

func TestBuildSystemPromptUsesCustomIdentityWithoutDuplicateHeading(t *testing.T) {
	cm := NewContextManager(t.TempDir())
	cm.files[FileIDENTITY] = "# Identity\n\nYou are a research assistant."
	prompt := cm.BuildSystemPrompt()
	if !strings.Contains(prompt, "## Identity\nYou are a research assistant.") || strings.Contains(prompt, "## Identity\n# Identity") {
		t.Fatalf("unexpected identity section:\n%s", prompt)
	}
}
