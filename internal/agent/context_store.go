package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-claw/internal/config"
	"go-claw/internal/llm"
)

const (
	microCompressionMarker  = "\n[tool_result compressed: original output remains reproducible by calling the tool again]"
	clearedToolResultMarker = "[tool_result cleared: call the tool again if this result is needed]"
)

type contextRecord struct {
	Kind      string       `json:"kind"`
	Turn      int          `json:"turn,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
	Message   *llm.Message `json:"message,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
	Summary   string       `json:"summary,omitempty"`
}

type ContextStore struct {
	dir   string
	cfg   config.ContextConfig
	locks sync.Map
}

func NewContextStore(dir string, cfg config.ContextConfig) *ContextStore {
	return &ContextStore{dir: dir, cfg: cfg}
}

func (s *ContextStore) enabled(sessionID uint) bool {
	return s != nil && s.cfg.Enabled && sessionID != 0
}

func (s *ContextStore) sessionLock(sessionID uint) *sync.Mutex {
	lock, _ := s.locks.LoadOrStore(sessionID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (s *ContextStore) path(sessionID uint) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.jsonl", sessionID))
}

func (s *ContextStore) load(sessionID uint) ([]contextRecord, error) {
	if !s.enabled(sessionID) {
		return nil, nil
	}
	f, err := os.Open(s.path(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []contextRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var record contextRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode context transcript: %w", err)
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func (s *ContextStore) LoadMessages(sessionID uint) ([]llm.Message, error) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	records, err := s.load(sessionID)
	if err != nil {
		return nil, err
	}
	// Build the model-visible context from newest to oldest. The latest summary
	// is a boundary: older records remain in JSONL for audit, but are no longer
	// sent to the model.
	messages := make([]llm.Message, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if record.Message != nil {
			messages = append(messages, *record.Message)
		} else if record.Kind == "summary" && strings.TrimSpace(record.Summary) != "" {
			// Read legacy summaries as virtual history messages without rewriting
			// the original transcript.
			messages = append(messages, llm.Message{
				Role:    "user",
				Content: "<conversation_summary>\n" + strings.TrimSpace(record.Summary) + "\n</conversation_summary>",
			})
		}
		if record.Kind == "summary" {
			break
		}
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

func (s *ContextStore) AppendTurn(sessionID uint, messages []llm.Message) error {
	if !s.enabled(sessionID) || len(messages) == 0 {
		return nil
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	records, err := s.load(sessionID)
	if err != nil {
		return err
	}
	turn := 1
	for _, record := range records {
		if record.Turn >= turn {
			turn = record.Turn + 1
		}
	}
	toolNames := map[string]string{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			toolNames[call.ID] = call.Function.Name
		}
		copyMessage := message
		records = append(records, contextRecord{
			Kind: "message", Turn: turn, Timestamp: time.Now(), Message: &copyMessage,
			ToolName: toolNames[message.ToolCallID],
		})
	}
	return s.write(sessionID, records)
}

func (s *ContextStore) SeedHistory(sessionID uint, messages []llm.Message) error {
	if !s.enabled(sessionID) || len(messages) == 0 {
		return nil
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	existing, err := s.load(sessionID)
	if err != nil || len(existing) > 0 {
		return err
	}
	turn := 0
	records := make([]contextRecord, 0, len(messages))
	for _, message := range messages {
		if message.Role == "user" || turn == 0 {
			turn++
		}
		copyMessage := message
		records = append(records, contextRecord{Kind: "message", Turn: turn, Timestamp: time.Now(), Message: &copyMessage})
	}
	return s.write(sessionID, records)
}

func (s *ContextStore) Compact(sessionID uint, systemPrompt, pendingInput string) (records []contextRecord, usage int, needsSummary bool, err error) {
	if !s.enabled(sessionID) {
		return nil, 0, false, nil
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	records, err = s.load(sessionID)
	if err != nil || len(records) == 0 {
		return records, 0, false, err
	}
	activeStart := latestSummaryIndex(records)
	active := records[activeStart:]
	recentTurns := s.cfg.RecentTurns
	if recentTurns <= 0 {
		recentTurns = 10
	}
	maxTurn := 0
	for _, record := range active {
		if record.Turn > maxTurn {
			maxTurn = record.Turn
		}
	}
	cutoff := maxTurn - recentTurns
	maxBytes := s.cfg.ToolResultMaxBytes
	if maxBytes <= 0 {
		maxBytes = 500
	}
	for i := range active {
		r := &active[i]
		if r.Turn > cutoff || r.Message == nil || r.Message.Role != "tool" || !isRepeatableTool(r.ToolName) {
			continue
		}
		if len(r.Message.Content) > maxBytes && !strings.Contains(r.Message.Content, "[tool_result compressed:") {
			r.Message.Content = truncateUTF8Bytes(r.Message.Content, maxBytes) + microCompressionMarker
		}
	}
	usage = s.usagePercent(active, systemPrompt, pendingInput)
	threshold := s.cfg.CompactThreshold
	if threshold <= 0 {
		threshold = 50
	}
	if usage > threshold {
		for i := range active {
			r := &active[i]
			if r.Turn <= cutoff && r.Message != nil && r.Message.Role == "tool" && isRepeatableTool(r.ToolName) && r.Message.Content != clearedToolResultMarker {
				r.Message.Content = clearedToolResultMarker
			}
		}
		usage = s.usagePercent(active, systemPrompt, pendingInput)
	}
	summaryThreshold := s.cfg.SummaryThreshold
	if summaryThreshold <= 0 {
		summaryThreshold = 90
	}
	return active, usage, usage > summaryThreshold, nil
}

func messagesFromContextRecords(records []contextRecord) []llm.Message {
	messages := make([]llm.Message, 0, len(records))
	for _, record := range records {
		if record.Message != nil {
			messages = append(messages, *record.Message)
		} else if record.Kind == "summary" && strings.TrimSpace(record.Summary) != "" {
			messages = append(messages, llm.Message{
				Role:    "user",
				Content: "<conversation_summary>\n" + strings.TrimSpace(record.Summary) + "\n</conversation_summary>",
			})
		}
	}
	return messages
}

func (s *ContextStore) ReplaceWithSummary(sessionID uint, summary string) error {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	message := &llm.Message{
		Role:    "user",
		Content: "<conversation_summary>\n" + strings.TrimSpace(summary) + "\n</conversation_summary>",
	}
	return s.appendRecord(sessionID, contextRecord{Kind: "summary", Turn: 0, Timestamp: time.Now(), Message: message})
}

func latestSummaryIndex(records []contextRecord) int {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Kind == "summary" {
			return i
		}
	}
	return 0
}

func (s *ContextStore) usagePercent(records []contextRecord, systemPrompt, pendingInput string) int {
	bytes := len(systemPrompt) + len(pendingInput)
	for _, record := range records {
		if record.Message != nil {
			bytes += len(record.Message.Content)
			for _, call := range record.Message.ToolCalls {
				bytes += len(call.Function.Name) + len(call.Function.Arguments)
			}
		} else {
			bytes += len(record.Summary)
		}
	}
	window := s.cfg.WindowTokens
	if window <= 0 {
		window = 200000
	}
	// Conservative language-independent approximation: ~3 UTF-8 bytes/token.
	return (bytes / 3) * 100 / window
}

func (s *ContextStore) write(sessionID uint, records []contextRecord) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	tmp := s.path(sessionID) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(sessionID))
}

func (s *ContextStore) appendRecord(sessionID uint, record contextRecord) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(sessionID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(record); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func isRepeatableTool(name string) bool {
	switch name {
	case "read_file", "list_dir", "web_search", "web_fetch", "get_current_time", "list_skills", "get_skill":
		return true
	default:
		return false
	}
}

func truncateUTF8Bytes(value string, max int) string {
	if len(value) <= max {
		return value
	}
	cut := max
	for cut > 0 && (value[cut]&0xC0) == 0x80 {
		cut--
	}
	return value[:cut]
}
