package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-claw/internal/storage"
	"go-claw/internal/tools"
)

// CreateScheduleTool implements the tool for creating scheduled jobs
type CreateScheduleTool struct {
	repo      *storage.Repository
	scheduler *Manager
}

// NewCreateScheduleTool creates a new schedule tool
func NewCreateScheduleTool(repo *storage.Repository, scheduler *Manager) *CreateScheduleTool {
	return &CreateScheduleTool{
		repo:      repo,
		scheduler: scheduler,
	}
}

// Info returns tool information
func (t *CreateScheduleTool) Info(ctx context.Context) (*tools.ToolInfo, error) {
	return &tools.ToolInfo{
		Name:        "create_schedule",
		Description: "Create a scheduled job. Supports: 1) One-time reminder (kind='at'), 2) Recurring task (kind='every' or kind='cron'). Session target: 'main' (default), 'isolated', 'current', or 'session:xxx'. Payload: {kind: 'systemEvent'|'agentTurn', input: string}",
		Parameters: tools.ToolParameters{
			Type: tools.Object,
			Properties: map[string]tools.ToolParameter{
				"name": {
					Type:        tools.String,
					Description: "Job name",
				},
				"description": {
					Type:        tools.String,
					Description: "Job description",
				},
				"schedule": {
					Type:        tools.Object,
					Description: "Schedule config: {kind: 'at'|'every'|'cron', time?: string (for 'at'), interval?: string (for 'every'), cron?: string (for 'cron')}",
				},
				"sessionTarget": {
					Type:        tools.String,
					Description: "Session target: 'main', 'isolated', 'current', 'session:xxx'",
					Default:     "main",
				},
				"payload": {
					Type:        tools.Object,
					Description: "Payload config: {kind: 'systemEvent'|'agentTurn', input: string}",
				},
				"deleteAfterRun": {
					Type:        tools.Boolean,
					Description: "Delete after successful run (for kind='at')",
					Default:     true,
				},
				"enabled": {
					Type:        tools.Boolean,
					Description: "Enable immediately",
					Default:     true,
				},
			},
			Required: []string{"name", "schedule", "payload"},
		},
	}, nil
}

// CreateScheduleParams represents the parameters for creating a schedule
type CreateScheduleParams struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Schedule      ScheduleConfig `json:"schedule"`
	SessionTarget string         `json:"sessionTarget"`
	Payload       PayloadConfig  `json:"payload"`
	DeleteAfter   bool           `json:"deleteAfterRun"`
	Enabled       bool           `json:"enabled"`
}

// ScheduleConfig represents schedule configuration
type ScheduleConfig struct {
	Kind     string `json:"kind"`
	Time     string `json:"time"`
	Interval string `json:"interval"`
	Cron     string `json:"cron"`
}

// PayloadConfig represents payload configuration
type PayloadConfig struct {
	Kind  string `json:"kind"`
	Input string `json:"input"`
}

// Invoke executes the tool
func (t *CreateScheduleTool) Invoke(ctx context.Context, params json.RawMessage, opt ...tools.Option) (*tools.ToolResult, error) {
	var p CreateScheduleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &tools.ToolResult{Text: "Failed to parse parameters: " + err.Error()}, nil
	}

	agentID, ok := ctx.Value("agent_id").(uint)
	if !ok || agentID == 0 {
		return &tools.ToolResult{Text: "Failed to get current agent ID"}, nil
	}

	// Get current session ID from context
	sessionID, _ := ctx.Value("session_id").(uint)

	task := &storage.ScheduledTask{
		Name:          p.Name,
		Description:   p.Description,
		AgentID:       agentID,
		SessionID:     sessionID, // Store the session ID for task execution
		Kind:          p.Schedule.Kind,
		SessionTarget: p.SessionTarget,
		PayloadKind:   p.Payload.Kind,
		Input:         p.Payload.Input,
		DeleteAfter:   p.DeleteAfter,
		Enabled:       p.Enabled,
	}

	if task.SessionTarget == "" {
		task.SessionTarget = "main"
	}

	if task.PayloadKind == "" {
		if task.SessionTarget == "isolated" {
			task.PayloadKind = "agentTurn"
		} else {
			task.PayloadKind = "systemEvent"
		}
	}

	switch p.Schedule.Kind {
	case "at":
		if p.Schedule.Time == "" {
			return &tools.ToolResult{Text: "Time required for 'at' schedule"}, nil
		}
		scheduledTime, err := parseTime(p.Schedule.Time)
		if err != nil {
			return &tools.ToolResult{Text: fmt.Sprintf("Invalid time: %v", err)}, nil
		}
		task.ScheduledAt = &scheduledTime
		task.NextRunAt = &scheduledTime
		task.CronExpr = fmt.Sprintf("%d %d %d %d %d", scheduledTime.Minute(), scheduledTime.Hour(), scheduledTime.Day(), scheduledTime.Month(), int(scheduledTime.Weekday()))
		task.DeleteAfter = true
	case "every":
		if p.Schedule.Interval == "" {
			return &tools.ToolResult{Text: "Interval required for 'every' schedule"}, nil
		}
		task.Interval = p.Schedule.Interval
		cronExpr, err := intervalToCron(p.Schedule.Interval)
		if err != nil {
			return &tools.ToolResult{Text: fmt.Sprintf("Invalid interval: %v", err)}, nil
		}
		task.CronExpr = cronExpr
	case "cron":
		if p.Schedule.Cron == "" {
			return &tools.ToolResult{Text: "Cron expression required for 'cron' schedule"}, nil
		}
		task.CronExpr = p.Schedule.Cron
	default:
		return &tools.ToolResult{Text: fmt.Sprintf("Invalid schedule kind: %s", p.Schedule.Kind)}, nil
	}

	if err := t.repo.CreateScheduledTask(task); err != nil {
		return &tools.ToolResult{Text: fmt.Sprintf("Failed to create schedule: %v", err)}, nil
	}

	// Add to scheduler if enabled
	if task.Enabled {
		if err := t.scheduler.AddTask(task); err != nil {
			return &tools.ToolResult{Text: fmt.Sprintf("Schedule created but failed to start: %v", err)}, nil
		}
	}

	result := fmt.Sprintf("Schedule created: %s (ID: %d)\nKind: %s\nSession: %s\nPayload: %s\nNext: %s",
		task.Name, task.ID, task.Kind, task.SessionTarget, task.PayloadKind, task.NextRunAt.Format("2006-01-02 15:04"))

	if task.Kind == "at" {
		result += fmt.Sprintf("\nTime: %s", task.ScheduledAt.Format("2006-01-02 15:04"))
		if task.DeleteAfter {
			result += "\n(Will delete after run)"
		}
	} else if task.Kind == "every" {
		result += fmt.Sprintf("\nInterval: %s", task.Interval)
	} else {
		result += fmt.Sprintf("\nCron: %s", task.CronExpr)
	}

	return &tools.ToolResult{Text: result}, nil
}

func parseTime(timeStr string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", timeStr); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local), nil
	}

	if dur, ok := parseRelativeTime(timeStr); ok {
		return time.Now().Add(dur), nil
	}

	now := time.Now()
	if t, err := time.Parse("15:04", timeStr); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), nil
	}
	if t, err := time.Parse("3pm", timeStr); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), 0, 0, 0, now.Location()), nil
	}
	if t, err := time.Parse("3:04pm", timeStr); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), nil
	}

	chineseTime := map[string]int{"凌晨": 0, "早上": 6, "上午": 9, "中午": 12, "下午": 13, "傍晚": 17, "晚上": 19, "深夜": 23}
	for prefix, _ := range chineseTime {
		if strings.HasPrefix(timeStr, prefix) {
			timeStr = strings.TrimPrefix(timeStr, prefix)
			if strings.HasSuffix(timeStr, "点") {
				hourStr := strings.TrimSuffix(timeStr, "点")
				if hour, err := strconv.Atoi(hourStr); err == nil {
					return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location()), nil
				}
			}
		}
	}

	if hour, err := strconv.Atoi(timeStr); err == nil && hour >= 0 && hour <= 23 {
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
		if t.Before(now) {
			t = t.Add(24 * time.Hour)
		}
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time: %s", timeStr)
}

func parseRelativeTime(timeStr string) (time.Duration, bool) {
	timeStr = strings.ToLower(timeStr)
	if strings.HasPrefix(timeStr, "in ") {
		timeStr = strings.TrimPrefix(timeStr, "in ")
		if strings.HasSuffix(timeStr, " minutes") || strings.HasSuffix(timeStr, " mins") || strings.HasSuffix(timeStr, " minute") {
			minutes, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(timeStr, " minutes"), " mins"), " minute"))
			return time.Duration(minutes) * time.Minute, true
		}
		if strings.HasSuffix(timeStr, " hours") || strings.HasSuffix(timeStr, " hour") {
			hours, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(timeStr, " hours"), " hour"))
			return time.Duration(hours) * time.Hour, true
		}
	}
	if strings.HasSuffix(timeStr, "分钟后") {
		minutes, _ := strconv.Atoi(strings.TrimSuffix(timeStr, "分钟后"))
		return time.Duration(minutes) * time.Minute, true
	}
	if strings.HasSuffix(timeStr, "小时后") {
		hours, _ := strconv.Atoi(strings.TrimSuffix(timeStr, "小时后"))
		return time.Duration(hours) * time.Hour, true
	}
	return 0, false
}

func intervalToCron(interval string) (string, error) {
	if strings.HasSuffix(interval, "h") {
		return "0 * * * *", nil
	}
	if strings.HasSuffix(interval, "d") {
		return "0 0 * * *", nil
	}
	if strings.HasSuffix(interval, "m") {
		return "* * * * *", nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}
