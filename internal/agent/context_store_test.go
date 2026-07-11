package agent

import (
	"strings"
	"testing"

	"go-claw/internal/config"
	"go-claw/internal/llm"
	"go-claw/internal/tools"
)

func testToolTurn(turn int, output string) []llm.Message {
	call := llm.ToolCall{ID: "call-" + string(rune('a'+turn)), Type: "function"}
	call.Function.Name = "read_file"
	call.Function.Arguments = `{"path":"example.txt"}`
	return []llm.Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		{Role: "tool", ToolCallID: call.ID, Content: output},
		{Role: "assistant", Content: "done"},
	}
}

func TestContextStoreMicroCompressesOldRepeatableToolResults(t *testing.T) {
	store := NewContextStore(t.TempDir(), config.ContextConfig{
		Enabled: true, WindowTokens: 1_000_000, RecentTurns: 10,
		ToolResultMaxBytes: 500, CompactThreshold: 50, SummaryThreshold: 90,
	})
	for turn := 0; turn < 12; turn++ {
		if err := store.AppendTurn(1, testToolTurn(turn, strings.Repeat("x", 700))); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := store.Compact(1, "", ""); err != nil {
		t.Fatal(err)
	}
	records, _, _, err := store.Compact(1, "", "")
	oldCompressed, recentUntouched := false, false
	for _, record := range records {
		if record.Message == nil || record.Message.Role != "tool" {
			continue
		}
		if record.Turn <= 2 && strings.Contains(record.Message.Content, "[tool_result compressed:") {
			oldCompressed = true
		}
		if record.Turn > 2 && len(record.Message.Content) == 700 {
			recentUntouched = true
		}
	}
	if !oldCompressed || !recentUntouched {
		t.Fatalf("unexpected compression state: old=%v recent=%v", oldCompressed, recentUntouched)
	}
	rawRecords, err := store.load(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range rawRecords {
		if record.Message != nil && record.Message.Role == "tool" && len(record.Message.Content) != 700 {
			t.Fatalf("compression must not modify raw JSONL: %q", record.Message.Content)
		}
	}
}

func TestContextStoreClearsOldResultsAboveHalfWindow(t *testing.T) {
	store := NewContextStore(t.TempDir(), config.ContextConfig{
		Enabled: true, WindowTokens: 10, RecentTurns: 1,
		ToolResultMaxBytes: 500, CompactThreshold: 50, SummaryThreshold: 101,
	})
	for turn := 0; turn < 2; turn++ {
		if err := store.AppendTurn(2, testToolTurn(turn, strings.Repeat("x", 700))); err != nil {
			t.Fatal(err)
		}
	}
	records, _, _, err := store.Compact(2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Turn == 1 && record.Message != nil && record.Message.Role == "tool" && record.Message.Content != clearedToolResultMarker {
			t.Fatalf("old tool result was not cleared: %q", record.Message.Content)
		}
	}
	rawRecords, _ := store.load(2)
	for _, record := range rawRecords {
		if record.Message != nil && record.Message.Role == "tool" && record.Message.Content == clearedToolResultMarker {
			t.Fatal("cleared marker must not be written to raw JSONL")
		}
	}
}

func TestContextStoreSummaryBecomesHistoryBoundary(t *testing.T) {
	store := NewContextStore(t.TempDir(), config.ContextConfig{Enabled: true, WindowTokens: 10, SummaryThreshold: 1, CompactThreshold: 101})
	if err := store.AppendTurn(3, []llm.Message{{Role: "user", Content: strings.Repeat("context", 20)}}); err != nil {
		t.Fatal(err)
	}
	_, _, needsSummary, err := store.Compact(3, "system", "pending")
	if err != nil || !needsSummary {
		t.Fatalf("expected summary trigger, needs=%v err=%v", needsSummary, err)
	}
	if err := store.ReplaceWithSummary(3, "The user chose option A."); err != nil {
		t.Fatal(err)
	}
	messages, err := store.LoadMessages(3)
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Content, "The user chose option A.") {
		t.Fatalf("unexpected summary boundary: messages=%v err=%v", messages, err)
	}
	records, err := store.load(3)
	if err != nil || len(records) != 2 || records[0].Kind == "summary" || records[1].Kind != "summary" || records[1].Message == nil || records[1].Message.Role != "user" {
		t.Fatalf("summary must be appended without deleting original history: records=%#v err=%v", records, err)
	}
	if err := store.AppendTurn(3, []llm.Message{{Role: "user", Content: "What next?"}, {Role: "assistant", Content: "Proceed with B."}}); err != nil {
		t.Fatal(err)
	}
	records, _ = store.load(3)
	if len(records) != 4 || records[1].Kind != "summary" || records[2].Message.Role != "user" {
		t.Fatalf("new history must continue after summary: %#v", records)
	}
	messages, err = store.LoadMessages(3)
	if err != nil || len(messages) != 3 || !strings.Contains(messages[0].Content, "conversation_summary") || messages[1].Content != "What next?" {
		t.Fatalf("model history must begin at latest summary: messages=%#v err=%v", messages, err)
	}
}

func TestContextStoreReadsLegacySummaryWithoutRewritingTranscript(t *testing.T) {
	store := NewContextStore(t.TempDir(), config.ContextConfig{Enabled: true})
	if err := store.write(4, []contextRecord{{Kind: "summary", Summary: "Legacy summary."}}); err != nil {
		t.Fatal(err)
	}
	messages, err := store.LoadMessages(4)
	if err != nil || len(messages) != 1 || messages[0].Role != "user" || !strings.Contains(messages[0].Content, "Legacy summary.") {
		t.Fatalf("legacy summary was not loaded as history: messages=%#v err=%v", messages, err)
	}
	records, err := store.load(4)
	if err != nil || records[0].Message != nil || records[0].Summary != "Legacy summary." {
		t.Fatalf("legacy transcript must remain unchanged: records=%#v err=%v", records, err)
	}
}

func TestTruncateUTF8BytesDoesNotSplitRune(t *testing.T) {
	got := truncateUTF8Bytes("你好世界", 5)
	if got != "你" {
		t.Fatalf("unexpected UTF-8 truncation: %q", got)
	}
}

func TestCalculateContextUsagePrefersProviderTokens(t *testing.T) {
	a := &Agent{manager: &Manager{cfg: &config.Config{Context: config.ContextConfig{WindowTokens: 1000}}}}
	usage := a.calculateContextUsage("system", []llm.Message{{Role: "user", Content: "hello"}}, "answer", nil, 400, 50)
	if usage.UsedTokens != 450 || usage.Percent != 45 || usage.Estimated {
		t.Fatalf("unexpected exact context usage: %#v", usage)
	}
}

func TestCalculateContextUsageFallsBackToEstimate(t *testing.T) {
	a := &Agent{manager: &Manager{cfg: &config.Config{Context: config.ContextConfig{WindowTokens: 1000}}}}
	usage := a.calculateContextUsage(
		strings.Repeat("s", 300),
		[]llm.Message{{Role: "user", Content: strings.Repeat("m", 300)}},
		strings.Repeat("a", 300),
		[]*tools.ToolInfo{{Name: "read_file", Description: "read a file"}},
		0, 0,
	)
	if usage.UsedTokens == 0 || usage.Percent == 0 || !usage.Estimated {
		t.Fatalf("unexpected estimated context usage: %#v", usage)
	}
}
