package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"go-claw/internal/llm"
	"go-claw/internal/tools"
)

const summaryAgentPrompt = `You are a conversation context summarizer. Produce a dense, faithful summary that another agent can use as the sole replacement for the conversation so far.

Preserve:
- user facts, preferences, constraints, intent, and unresolved requests
- decisions, conclusions, plans, completed work, and current state
- important file paths, identifiers, commands, errors, and verified results
- tool-derived facts that cannot safely be inferred again
- open tasks and the exact next action

Remove repetition, greetings, verbose tool output, and obsolete intermediate reasoning. Do not invent facts. Output only the summary in Markdown.`

func (a *Agent) prepareConversationContext(ctx context.Context, req ExecuteRequest, systemPrompt string) ([]llm.Message, error) {
	if req.ContextEmpty {
		return nil, nil
	}
	if req.SkipContextCompression || a.manager.contextStore == nil || !a.manager.contextStore.enabled(req.SessionID) {
		return a.databaseHistory(req.SessionID)
	}

	messages, err := a.manager.contextStore.LoadMessages(req.SessionID)
	if err != nil {
		slog.Warn("failed to load context transcript; falling back to database history", "session_id", req.SessionID, "error", err)
		return a.databaseHistory(req.SessionID)
	}
	if len(messages) == 0 {
		dbMessages, err := a.databaseHistory(req.SessionID)
		if err != nil {
			return nil, err
		}
		if err := a.manager.contextStore.SeedHistory(req.SessionID, dbMessages); err != nil {
			slog.Warn("failed to seed context transcript; using database history", "session_id", req.SessionID, "error", err)
			return dbMessages, nil
		}
	}

	records, usage, needsSummary, err := a.manager.contextStore.Compact(req.SessionID, systemPrompt, req.Input)
	if err != nil {
		slog.Warn("context compaction failed; falling back to database history", "session_id", req.SessionID, "error", err)
		return a.databaseHistory(req.SessionID)
	}
	if needsSummary {
		summary, err := a.summarizeContext(ctx, records)
		if err != nil {
			slog.Warn("context summary failed; continuing with compacted transcript", "session_id", req.SessionID, "usage_percent", usage, "error", err)
			return messagesFromContextRecords(records), nil
		}
		if err := a.manager.contextStore.ReplaceWithSummary(req.SessionID, summary); err != nil {
			slog.Warn("failed to save context summary; continuing with compacted transcript", "session_id", req.SessionID, "error", err)
			return messagesFromContextRecords(records), nil
		}
		slog.Info("conversation context summarized", "session_id", req.SessionID, "usage_percent", usage)
		return a.manager.contextStore.LoadMessages(req.SessionID)
	}
	return messagesFromContextRecords(records), nil
}

func (a *Agent) databaseHistory(sessionID uint) ([]llm.Message, error) {
	history, err := a.getConversationHistory(sessionID)
	if err != nil {
		return nil, err
	}
	messages := make([]llm.Message, 0, len(history))
	for _, msg := range history {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		messages = append(messages, llm.Message{Role: role, Content: msg.Content})
	}
	return messages, nil
}

func (a *Agent) summarizeContext(ctx context.Context, records []contextRecord) (string, error) {
	var transcript strings.Builder
	for _, record := range records {
		if record.Kind == "summary" {
			transcript.WriteString("Previous conversation summary:\n")
			if record.Message != nil {
				transcript.WriteString(record.Message.Content)
			} else {
				transcript.WriteString(record.Summary)
			}
			transcript.WriteString("\n\n")
			continue
		}
		if record.Message == nil {
			continue
		}
		transcript.WriteString(record.Message.Role)
		if record.ToolName != "" {
			transcript.WriteString(" (")
			transcript.WriteString(record.ToolName)
			transcript.WriteString(")")
		}
		transcript.WriteString(": ")
		transcript.WriteString(record.Message.Content)
		for _, call := range record.Message.ToolCalls {
			transcript.WriteString(fmt.Sprintf("\n[tool_call %s: %s]", call.Function.Name, call.Function.Arguments))
		}
		transcript.WriteString("\n\n")
	}

	agent, cleanup, err := a.manager.CreateTempAgent(&TempAgentOptions{
		Model:        a.getModel(),
		AllowedTools: []string{"__summary_agent_has_no_tools__"},
		Prompt:       summaryAgentPrompt,
	})
	if err != nil {
		return "", err
	}
	defer cleanup()
	result, err := agent.Execute(ctx, ExecuteRequest{
		Input:                  transcript.String(),
		ContextEmpty:           true,
		SkipMemoryExtraction:   true,
		SkipContextCompression: true,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.Content) == "" {
		return "", fmt.Errorf("summary agent returned empty content")
	}
	return result.Content, nil
}

func (a *Agent) persistContextTurn(sessionID uint, turnMessages []llm.Message, finalContent string) {
	if a.manager.contextStore == nil || !a.manager.contextStore.enabled(sessionID) {
		return
	}
	messages := append([]llm.Message(nil), turnMessages...)
	if finalContent != "" {
		messages = append(messages, llm.Message{Role: "assistant", Content: finalContent})
	}
	if err := a.manager.contextStore.AppendTurn(sessionID, messages); err != nil {
		slog.Warn("failed to persist context transcript", "session_id", sessionID, "error", err)
	}
}

func (a *Agent) calculateContextUsage(systemPrompt string, messages []llm.Message, finalContent string, toolList []*tools.ToolInfo, lastInputTokens, lastOutputTokens int) llm.ContextUsage {
	window := a.manager.cfg.Context.WindowTokens
	if window <= 0 {
		window = 200000
	}
	used := lastInputTokens + lastOutputTokens
	estimated := false
	if lastInputTokens <= 0 {
		estimated = true
		bytes := len(systemPrompt) + len(finalContent)
		for _, message := range messages {
			bytes += len(message.Role) + len(message.Content) + len(message.ToolCallID)
			for _, call := range message.ToolCalls {
				bytes += len(call.Function.Name) + len(call.Function.Arguments)
			}
		}
		if encoded, err := json.Marshal(toolList); err == nil {
			bytes += len(encoded)
		}
		used = bytes / 3
	}
	percent := math.Round((float64(used)*100/float64(window))*100) / 100
	return llm.ContextUsage{UsedTokens: used, WindowTokens: window, Percent: percent, Estimated: estimated}
}
