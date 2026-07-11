package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryCaptureToolReadsMemoryFromArguments(t *testing.T) {
	capture := &memoryCaptureTool{}
	_, err := capture.Invoke(context.Background(), json.RawMessage(`{"type":"semantic","summary":"项目使用 Go","content":"项目后端使用 Go。"}`))
	if err != nil {
		t.Fatal(err)
	}
	memories := capture.snapshot()
	if len(memories) != 1 || memories[0].Summary != "项目使用 Go" {
		t.Fatalf("unexpected memories: %#v", memories)
	}
	if _, err := capture.Invoke(context.Background(), json.RawMessage(`{"type":"semantic","summary":"项目使用 Go","content":"项目后端使用 Go。"}`)); err != nil {
		t.Fatal(err)
	}
	if len(capture.snapshot()) != 1 {
		t.Fatal("duplicate tool calls must be deduplicated")
	}
}

func TestMemoryCaptureToolRejectsInvalidArguments(t *testing.T) {
	capture := &memoryCaptureTool{}
	if _, err := capture.Invoke(context.Background(), json.RawMessage(`{"type":"unknown","summary":"忽略","content":"无效"}`)); err == nil {
		t.Fatal("expected invalid memory type to fail")
	}
	if len(capture.snapshot()) != 0 {
		t.Fatal("invalid memory must not be captured")
	}
}

func TestStoreMemoriesUpdatesFilesAndIndex(t *testing.T) {
	workspace := t.TempDir()
	manager := &Manager{
		workspace:      workspace,
		contextManager: NewContextManager(workspace),
	}
	when := time.Date(2026, 7, 11, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	err := manager.storeMemories([]extractedMemory{{
		Type: "fact_preference", Summary: "用户偏好简洁回答", Content: "用户偏好简洁、直接的回答。",
	}}, when)
	if err != nil {
		t.Fatal(err)
	}

	memoryPath := filepath.Join(workspace, "memory", "用户偏好简洁回答.md")
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "timestamp: 2026-07-11T10:30:00+08:00") || !strings.Contains(content, "用户偏好简洁、直接的回答。") {
		t.Fatalf("unexpected memory file: %s", content)
	}

	index, err := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "- 用户偏好简洁、直接的回答。") ||
		!strings.Contains(string(index), "[详细记忆](memory/用户偏好简洁回答.md)") ||
		!strings.Contains(manager.contextManager.Get(FileMEMORY), "核心内容不足以回答") {
		t.Fatalf("unexpected index: %s", index)
	}
}

func TestMemoryIndexContentIsCompact(t *testing.T) {
	content := "---\ntimestamp: now\nsummary: test\ntype: semantic\n---\n\n第一行\n\n第二行"
	if got := memoryIndexContent(content); got != "第一行 第二行" {
		t.Fatalf("unexpected index content: %q", got)
	}
}

func TestSafeMemoryFilename(t *testing.T) {
	if got := safeMemoryFilename(`偏好: Go/TypeScript?`); got != "偏好- Go-TypeScript" {
		t.Fatalf("unexpected safe filename: %q", got)
	}
}
