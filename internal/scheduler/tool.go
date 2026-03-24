package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-claw/internal/notify"
	"go-claw/internal/storage"
	"go-claw/internal/tools"
)

type CreateScheduleTool struct {
	repo      *storage.Repository
	scheduler *Manager
}

func NewCreateScheduleTool(repo *storage.Repository, scheduler *Manager) *CreateScheduleTool {
	return &CreateScheduleTool{
		repo:      repo,
		scheduler: scheduler,
	}
}

func (t *CreateScheduleTool) Info(ctx context.Context) (*tools.ToolInfo, error) {
	return &tools.ToolInfo{
		Name: "create_schedule",
		Description: `Create a scheduled task. Choose ONE of the following modes:

1. ONE-TIME TASK (runAt): Execute once at a specific time, auto-delete after completion.
   - Relative time: "3m", "1h", "2 hours", "in 5 minutes"
   - Absolute time: "2026-01-01 10:00", "tomorrow 9am", "18:30"

2. CRON TASK (cron): Execute on a schedule using cron expression.
   - 5-field format: "0 9 * * *" (every day at 9am)
   - "*/30 * * * *" (every 30 minutes)

3. INTERVAL TASK (interval): Execute at regular intervals.
   - "30m", "1h", "2h", "1d"

Examples:
- {"runAt": "5m", "input": "remind me to take a break"} - remind in 5 minutes
- {"cron": "0 9 * * *", "input": "daily standup reminder"} - every day at 9am
- {"interval": "1h", "input": "hourly status check"} - every hour`,
		Parameters: tools.ToolParameters{
			Type: tools.Object,
			Properties: map[string]tools.ToolParameter{
				"runAt": {
					Type:        tools.String,
					Description: "One-time task: when to run. Supports relative time ('3m', '1h', 'in 5 minutes') or absolute time ('2026-01-01 10:00', 'tomorrow 9am'). Task will be deleted after execution.",
				},
				"cron": {
					Type:        tools.String,
					Description: "Cron expression for recurring tasks (5 fields: minute hour day month weekday). Example: '0 9 * * *' for daily at 9am.",
				},
				"interval": {
					Type:        tools.String,
					Description: "Interval for recurring tasks. Examples: '30m', '1h', '2h', '1d'.",
				},
				"input": {
					Type:        tools.String,
					Description: "The content/command to execute when the task triggers. This is required.",
				},
				"name": {
					Type:        tools.String,
					Description: "Optional task name. Will be auto-generated if not provided.",
				},
			},
			Required: []string{"input"},
		},
	}, nil
}

type CreateScheduleParams struct {
	Name     string `json:"name"`
	RunAt    string `json:"runAt"`
	Cron     string `json:"cron"`
	Interval string `json:"interval"`
	Input    string `json:"input"`
}

func (t *CreateScheduleTool) Invoke(ctx context.Context, params json.RawMessage, opt ...tools.Option) (*tools.ToolResult, error) {
	var p CreateScheduleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &tools.ToolResult{Text: "Failed to parse parameters: " + err.Error()}, nil
	}

	if p.Input == "" {
		return &tools.ToolResult{Text: "Error: 'input' is required."}, nil
	}

	modeCount := 0
	if p.RunAt != "" {
		modeCount++
	}
	if p.Cron != "" {
		modeCount++
	}
	if p.Interval != "" {
		modeCount++
	}
	if modeCount == 0 {
		return &tools.ToolResult{Text: "Error: Must provide one of: 'runAt', 'cron', or 'interval'."}, nil
	}
	if modeCount > 1 {
		return &tools.ToolResult{Text: "Error: Can only use one of: 'runAt', 'cron', or 'interval'."}, nil
	}

	agentID, ok := ctx.Value("agent_id").(uint)
	if !ok || agentID == 0 {
		return &tools.ToolResult{Text: "Failed to get current agent ID"}, nil
	}

	sessionID, _ := ctx.Value("session_id").(uint)

	platform, platformChatID := notify.GetPlatformFromContext(ctx)

	task := &storage.ScheduledTask{
		Name:           p.Name,
		AgentID:        agentID,
		SessionID:      sessionID,
		SessionTarget:  "main",
		PayloadKind:    "systemEvent",
		Input:          p.Input,
		Enabled:        true,
		Platform:       platform,
		PlatformChatID: platformChatID,
	}

	if task.Name == "" {
		task.Name = fmt.Sprintf("Task-%d", time.Now().Unix())
	}

	var nextRunStr string
	var err error

	switch {
	case p.RunAt != "":
		task.Kind = "at"
		task.DeleteAfter = true
		nextRunStr, err = t.setupRunAtTask(task, p.RunAt)
	case p.Cron != "":
		task.Kind = "cron"
		task.CronExpr = p.Cron
		nextRunStr, err = t.setupCronTask(task)
	case p.Interval != "":
		task.Kind = "every"
		task.Interval = p.Interval
		nextRunStr, err = t.setupIntervalTask(task, p.Interval)
	}

	if err != nil {
		return &tools.ToolResult{Text: err.Error()}, nil
	}

	if err := t.repo.CreateScheduledTask(task); err != nil {
		return &tools.ToolResult{Text: fmt.Sprintf("Failed to create task: %v", err)}, nil
	}

	if task.Enabled {
		if err := t.scheduler.AddTask(task); err != nil {
			return &tools.ToolResult{Text: fmt.Sprintf("Task created but failed to start: %v", err)}, nil
		}
	}

	modeDesc := ""
	switch task.Kind {
	case "at":
		modeDesc = "One-time task"
	case "cron":
		modeDesc = "Cron task"
	case "every":
		modeDesc = "Interval task"
	}

	result := fmt.Sprintf("✅ Task created: **%s**\nType: %s\nNext run: %s\nContent: %s",
		task.Name, modeDesc, nextRunStr, task.Input)

	return &tools.ToolResult{Text: result}, nil
}

func (t *CreateScheduleTool) setupRunAtTask(task *storage.ScheduledTask, runAt string) (string, error) {
	runTime, err := parseTime(runAt)
	if err != nil {
		return "", fmt.Errorf("invalid time '%s': %v", runAt, err)
	}

	if runTime.Before(time.Now()) {
		return "", fmt.Errorf("scheduled time '%s' is in the past", runAt)
	}

	task.ScheduledAt = &runTime
	task.NextRunAt = &runTime
	task.CronExpr = fmt.Sprintf("%d %d %d %d %d",
		runTime.Minute(), runTime.Hour(), runTime.Day(), runTime.Month(), int(runTime.Weekday()))

	return runTime.Format("2006-01-02 15:04"), nil
}

func (t *CreateScheduleTool) setupCronTask(task *storage.ScheduledTask) (string, error) {
	if err := validateCronExpr(task.CronExpr); err != nil {
		return "", fmt.Errorf("invalid cron expression: %v", err)
	}

	nextRun, err := t.scheduler.GetNextRun(task.CronExpr)
	if err != nil {
		return "", fmt.Errorf("failed to calculate next run time: %v", err)
	}

	task.NextRunAt = &nextRun
	return nextRun.Format("2006-01-02 15:04"), nil
}

func (t *CreateScheduleTool) setupIntervalTask(task *storage.ScheduledTask, interval string) (string, error) {
	cronExpr, err := intervalToCron(interval)
	if err != nil {
		return "", err
	}

	task.CronExpr = cronExpr

	nextRun, err := t.scheduler.GetNextRun(task.CronExpr)
	if err != nil {
		return "", fmt.Errorf("failed to calculate next run time: %v", err)
	}

	task.NextRunAt = &nextRun
	return fmt.Sprintf("%s (next: %s)", interval, nextRun.Format("2006-01-02 15:04")), nil
}

func validateCronExpr(expr string) error {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return fmt.Errorf("cron expression must have 5 fields (minute hour day month weekday), got %d", len(parts))
	}
	return nil
}

func parseTime(timeStr string) (time.Time, error) {
	timeStr = strings.TrimSpace(timeStr)
	now := time.Now()

	if dur, ok := parseDuration(timeStr); ok {
		return now.Add(dur), nil
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04",
		"01/02 15:04",
		"2006-01-02",
		"15:04",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			if format == "15:04" || format == "01/02 15:04" {
				year := now.Year()
				if t.Before(now) {
					if format == "15:04" {
						t = t.Add(24 * time.Hour)
					} else {
						year++
					}
				}
				t = time.Date(year, t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			}
			return t, nil
		}
	}

	lowerStr := strings.ToLower(timeStr)
	if strings.Contains(lowerStr, "tomorrow") {
		tomorrow := now.AddDate(0, 0, 1)
		timePart := strings.TrimSpace(strings.TrimPrefix(lowerStr, "tomorrow"))
		if timePart == "" {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, now.Location()), nil
		}
		if t, err := time.Parse("15:04", timePart); err == nil {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), nil
		}
		if t, err := time.Parse("3pm", timePart); err == nil {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), t.Hour(), 0, 0, 0, now.Location()), nil
		}
	}

	if hour, err := strconv.Atoi(timeStr); err == nil && hour >= 0 && hour <= 23 {
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
		if t.Before(now) {
			t = t.Add(24 * time.Hour)
		}
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %s", timeStr)
}

func parseDuration(s string) (time.Duration, bool) {
	s = strings.ToLower(strings.TrimSpace(s))

	s = strings.TrimPrefix(s, "in ")
	s = strings.TrimPrefix(s, "after ")

	s = strings.ReplaceAll(s, "分钟", "m")
	s = strings.ReplaceAll(s, "小时", "h")
	s = strings.ReplaceAll(s, "天", "d")
	s = strings.ReplaceAll(s, "seconds", "s")
	s = strings.ReplaceAll(s, "second", "s")
	s = strings.ReplaceAll(s, "minutes", "m")
	s = strings.ReplaceAll(s, "minute", "m")
	s = strings.ReplaceAll(s, "mins", "m")
	s = strings.ReplaceAll(s, "min", "m")
	s = strings.ReplaceAll(s, "hours", "h")
	s = strings.ReplaceAll(s, "hour", "h")
	s = strings.ReplaceAll(s, "days", "d")
	s = strings.ReplaceAll(s, "day", "d")

	s = strings.TrimSpace(s)

	if dur, err := time.ParseDuration(s); err == nil {
		return dur, true
	}

	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		if num, err := strconv.Atoi(numStr); err == nil && num > 0 {
			return time.Duration(num) * 24 * time.Hour, true
		}
	}

	return 0, false
}

func intervalToCron(interval string) (string, error) {
	interval = strings.ToLower(strings.TrimSpace(interval))

	if dur, ok := parseDuration(interval); ok {
		totalMinutes := int(dur.Minutes())
		if totalMinutes < 1 {
			totalMinutes = 1
		}
		if totalMinutes < 60 {
			return fmt.Sprintf("*/%d * * * *", totalMinutes), nil
		}
		if totalMinutes < 1440 {
			hours := totalMinutes / 60
			return fmt.Sprintf("0 */%d * * *", hours), nil
		}
		if totalMinutes == 1440 {
			return "0 0 * * *", nil
		}
		days := totalMinutes / 1440
		return fmt.Sprintf("0 0 */%d * *", days), nil
	}

	return "", fmt.Errorf("invalid interval format: %s (use '30m', '1h', '1d', etc.)", interval)
}
