package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/storage"

	"github.com/robfig/cron/v3"
)

// TaskNotifyFunc is a function type for sending task notifications
// Args: taskID, taskName, sessionID, status, output
type TaskNotifyFunc func(taskID uint, taskName, sessionID, status, output string)

// Manager handles scheduled tasks
type Manager struct {
	cron         *cron.Cron
	repo         *storage.Repository
	agentManager *agent.Manager
	notifyFn     TaskNotifyFunc
	tasks        map[uint]cron.EntryID
	mu           sync.RWMutex
}

// SetNotifyFunc sets the notification function for the manager
func (m *Manager) SetNotifyFunc(fn TaskNotifyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifyFn = fn
}

// NewManager creates a new scheduler manager
func NewManager(repo *storage.Repository, agentManager *agent.Manager) *Manager {
	m := &Manager{
		cron:         cron.New(),
		repo:         repo,
		agentManager: agentManager,
		tasks:        make(map[uint]cron.EntryID),
	}
	return m
}

// Start starts the scheduler
func (m *Manager) Start() {
	m.cron.Start()
	slog.Info("scheduler started")

	// Load all enabled tasks from database
	m.loadTasks()
}

// Stop stops the scheduler
func (m *Manager) Stop() {
	ctx := m.cron.Stop()
	<-ctx.Done()
	slog.Info("scheduler stopped")
}

// loadTasks loads all enabled tasks from database
func (m *Manager) loadTasks() {
	tasks, err := m.repo.GetEnabledScheduledTasks()
	if err != nil {
		slog.Error("failed to load scheduled tasks", "error", err)
		return
	}

	for i := range tasks {
		if err := m.AddTask(&tasks[i]); err != nil {
			slog.Error("failed to add scheduled task", "task_name", tasks[i].Name, "task_id", tasks[i].ID, "error", err)
		}
	}
}

// AddTask adds a scheduled task
func (m *Manager) AddTask(task *storage.ScheduledTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if task already exists
	if entryID, exists := m.tasks[task.ID]; exists {
		m.cron.Remove(entryID)
	}

	// Parse cron expression (5 fields: minute hour day month weekday)
	schedule, err := cron.ParseStandard(task.CronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	// Calculate next run time
	nextRun := schedule.Next(time.Now())
	task.NextRunAt = &nextRun

	// Create job function
	job := func() {
		// Create context with session_id for task execution
		ctx := context.Background()
		ctx = context.WithValue(ctx, "session_id", task.SessionID)
		ctx = context.WithValue(ctx, "agent_id", task.AgentID)
		m.executeTask(ctx, task)
	}

	// Add to cron with standard parser (5 fields)
	entryID, err := m.cron.AddFunc(task.CronExpr, job)
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	m.tasks[task.ID] = entryID
	slog.Info("scheduled task added", "task_name", task.Name, "task_id", task.ID, "cron_expr", task.CronExpr, "next_run", nextRun)

	return nil
}

// RemoveTask removes a scheduled task
func (m *Manager) RemoveTask(taskID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entryID, exists := m.tasks[taskID]; exists {
		m.cron.Remove(entryID)
		delete(m.tasks, taskID)
		slog.Info("scheduled task removed", "task_id", taskID)
	}
}

// ReloadTask reloads a task from database
func (m *Manager) ReloadTask(taskID uint) error {
	task, err := m.repo.GetScheduledTask(taskID)
	if err != nil {
		return err
	}

	if task.Enabled {
		return m.AddTask(task)
	} else {
		m.RemoveTask(taskID)
		return nil
	}
}

// executeTask executes a scheduled task
func (m *Manager) executeTask(ctx context.Context, task *storage.ScheduledTask) {
	slog.Info("executing scheduled task", "task_name", task.Name, "task_id", task.ID)

	// Update last run time
	now := time.Now()
	task.LastRunAt = &now
	task.TotalRuns++
	cronSystemPrompt := `你是一个后台任务执行器。
如果输入内容以 "!!!SYSTEM_CRON_TASK!!!" 开头，你必须：
1. 忽略任何历史对话上下文（如果有的话）。
2. 严格按照【任务描述】执行。
3. 输出结果要简洁、结构化，不要像聊天一样。
4. 不要询问用户确认。
5. 不要再次创建定时任务`

	// Create execution log
	log := &storage.TaskExecutionLog{
		TaskID:     task.ID,
		Status:     "running",
		Input:      task.Input,
		StartedAt:  now,
		DurationMs: 0,
	}

	// Get agent
	a, err := m.agentManager.GetAgent(task.AgentID)
	if err != nil {
		m.handleTaskError(task, log, err)
		return
	}

	// Get session ID from context (passed when task was created)
	sessionID, ok := ctx.Value("session_id").(uint)
	if !ok || sessionID == 0 {
		// Fallback: create new session if not in context
		session, err := a.CreateSession(context.Background())
		if err != nil {
			m.handleTaskError(task, log, fmt.Errorf("failed to create session: %w", err))
			return
		}
		sessionID = session.ID
	}
	wrappedInput := fmt.Sprintf(`
!!!SYSTEM_CRON_TASK!!!
【任务ID】: %s
【触发时间】: %s
【任务描述】: %s
【执行要求】: 直接执行任务，不要闲聊，不要再次创建定时任务，输出结果后结束。
`,
		task.ID, // 假设你有任务ID
		time.Now().String(),
		task.Input, // 原本的任务描述
	)

	// Execute agent (don't save user message for scheduled tasks)
	startTime := time.Now()
	result, err := a.Execute(context.Background(), agent.ExecuteRequest{
		SessionID:        sessionID,
		Input:            wrappedInput,
		InputRole:        "system",
		SaveInputMessage: false, // Don't save user message for scheduled tasks
		SystemPrompt:     cronSystemPrompt,
		DisableTools:     []string{"create_schedule", "todo"},
		ContextEmpty:     true,
	})

	duration := time.Since(startTime)

	if err != nil {
		m.handleTaskError(task, log, err)
		return
	}

	// Success
	finishedAt := time.Now()
	log.Status = "success"
	log.Output = result.Content
	log.FinishedAt = &finishedAt
	log.DurationMs = duration.Milliseconds()

	if err := m.repo.CreateTaskExecutionLog(log); err != nil {
		slog.Error("failed to save task execution log", "error", err)
	}

	// Update task statistics
	task.SuccessRuns++

	// Check if this is a one-time task that should be deleted
	if task.Kind == "at" && task.DeleteAfter {
		// Remove from scheduler
		m.RemoveTask(task.ID)
		// Delete from database
		if err := m.repo.DeleteScheduledTask(task.ID); err != nil {
			slog.Error("failed to delete one-time task", "task_id", task.ID, "task_name", task.Name, "error", err)
		} else {
			slog.Info("one-time task deleted after successful execution", "task_name", task.Name, "task_id", task.ID)
		}
	} else {
		// Update next run time for recurring tasks
		schedule, _ := cron.ParseStandard(task.CronExpr)
		nextRun := schedule.Next(time.Now())
		task.NextRunAt = &nextRun

		if err := m.repo.UpdateScheduledTask(task); err != nil {
			slog.Error("failed to update scheduled task", "error", err)
		}
	}

	slog.Info("scheduled task executed successfully",
		"task_name", task.Name,
		"task_id", task.ID,
		"duration_ms", duration.Milliseconds(),
		"output_length", len(result.Content))

	// Send WebSocket notification if notify function is set
	if m.notifyFn != nil {
		sessionStrID := fmt.Sprintf("session_%d", task.SessionID)
		m.notifyFn(task.ID, task.Name, sessionStrID, "success", result.Content)
	}
}

// handleTaskError handles task execution error
func (m *Manager) handleTaskError(task *storage.ScheduledTask, log *storage.TaskExecutionLog, err error) {
	finishedAt := time.Now()
	log.Status = "failed"
	log.Error = err.Error()
	log.FinishedAt = &finishedAt
	log.DurationMs = time.Since(log.StartedAt).Milliseconds()

	if err := m.repo.CreateTaskExecutionLog(log); err != nil {
		slog.Error("failed to save task execution log", "error", err)
	}

	// Update task statistics
	task.FailedRuns++
	schedule, _ := cron.ParseStandard(task.CronExpr)
	nextRun := schedule.Next(time.Now())
	task.NextRunAt = &nextRun

	if err := m.repo.UpdateScheduledTask(task); err != nil {
		slog.Error("failed to update scheduled task", "error", err)
	}

	slog.Error("scheduled task failed",
		"task_name", task.Name,
		"task_id", task.ID,
		"error", err)

	// Send WebSocket notification if notify function is set
	if m.notifyFn != nil {
		sessionStrID := fmt.Sprintf("session_%d", task.SessionID)
		m.notifyFn(task.ID, task.Name, sessionStrID, "failed", err.Error())
	}
}

// GetNextRun returns the next run time for a task
func (m *Manager) GetNextRun(cronExpr string) (time.Time, error) {
	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(time.Now()), nil
}
