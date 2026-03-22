package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/storage"
)

var currentSession *storage.Session
var sessionAgent *agent.Agent
var sessionManager *agent.SessionManager

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	db, err := storage.Init(cfg)
	if err != nil {
		fmt.Printf("Failed to init database: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil && sqlDB != nil {
			sqlDB.Close()
		}
	}()

	repo := storage.NewRepository(db)
	agentManager := agent.NewManager(cfg, repo, cfg.WorkDir)
	sessionManager = agentManager.GetSessionManager()

	agents, err := agentManager.ListAgents()
	if err != nil || len(agents) == 0 {
		fmt.Println("No agents found. Creating default agent...")
		_, err = agentManager.CreateAgent("cli-agent", "CLI Agent", "", "")
		if err != nil {
			fmt.Printf("Failed to create agent: %v\n", err)
			os.Exit(1)
		}
		agents, _ = agentManager.ListAgents()
	}

	sessionAgent, _ = agentManager.GetAgent(agents[0].ID)
	session, err := sessionAgent.CreateSession(context.Background())
	if err != nil {
		fmt.Printf("Failed to create session: %v\n", err)
		os.Exit(1)
	}
	currentSession = session

	printBanner()
	showSessionInfo()

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(colorGreen("You") + ": ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			cmd := strings.ToLower(input)
			switch {
			case cmd == "/exit", cmd == "/quit", cmd == "/q":
				fmt.Println(colorCyan("Goodbye!"))
				return

			case cmd == "/clear", cmd == "/reset":
				clearConversation()
				continue

			case cmd == "/new", cmd == "/n":
				newConversation()

			case cmd == "/sessions", cmd == "/history", cmd == "/h":
				listSessions()

			case cmd == "/switch" || strings.HasPrefix(cmd, "/switch "):
				parts := strings.SplitN(input, " ", 2)
				if len(parts) == 2 {
					switchSession(parts[1])
				} else {
					fmt.Println("Usage: /switch <session_id>")
				}
				continue

			case cmd == "/delete" || strings.HasPrefix(cmd, "/delete "):
				parts := strings.SplitN(input, " ", 2)
				if len(parts) == 2 {
					deleteSession(parts[1])
				} else {
					fmt.Println("Usage: /delete <session_id>")
				}
				continue

			case cmd == "/tools", cmd == "/t":
				listTools(agentManager)
				continue

			case cmd == "/help", cmd == "/?":
				printHelp()
				continue

			case cmd == "/exec" || strings.HasPrefix(cmd, "/exec "):
				parts := strings.SplitN(input, " ", 2)
				if len(parts) == 2 {
					execCommand(parts[1])
				} else {
					fmt.Println("Usage: /exec <command>")
				}
				continue

			default:
				fmt.Printf("Unknown command: %s\n", input)
				printHelp()
				continue
			}
		}

		ctx := context.Background()
		fmt.Println()
		fmt.Print(colorYellow("Agent") + ": (thinking...)\n")
		result, err := sessionAgent.Execute(ctx, agent.ExecuteRequest{
			SessionID:        currentSession.ID,
			Input:            input,
			SaveInputMessage: true,
		})

		if err != nil {
			fmt.Printf("%s Error: %v\n\n", colorRed("✗"), err)
			continue
		}

		if len(result.ToolCalls) > 0 {
			fmt.Println(colorCyan("─── Tool Calls ───"))
			for _, tc := range result.ToolCalls {
				status := colorGreen("✓")
				if !tc.Success {
					status = colorRed("✗")
				}
				fmt.Printf("%s %s(%s)\n", status, tc.ToolName, tc.Input)
				if tc.Success {
					output := tc.Output
					if len(output) > 300 {
						output = output[:300] + colorDim("...(truncated)")
					}
					fmt.Printf("  → %s\n", output)
				} else {
					fmt.Printf("  → %s\n", colorRed(tc.Output))
				}
			}
			fmt.Println()
		}

		if result.Content != "" {
			fmt.Printf("%s\n\n", result.Content)
		}
	}
}

func printBanner() {
	fmt.Println(colorCyan("╔════════════════════════════════════════════════════════╗"))
	fmt.Println(colorCyan("║") + colorWhite("           Go-Claw Agent CLI v1.0") + colorCyan("                    ║"))
	fmt.Println(colorCyan("╠════════════════════════════════════════════════════════╣"))
	fmt.Println(colorCyan("║") + colorWhite("  Commands:                                                ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /new       - Start a new conversation                ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /sessions  - List all sessions                        ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /switch <id>- Switch to another session              ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /clear     - Clear current conversation              ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /delete <id>- Delete a session                       ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /tools     - List available tools                     ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /exec <cmd> - Execute shell command                   ") + colorCyan("║"))
	fmt.Println(colorCyan("║") + colorDim("    /exit      - Exit CLI                                  ") + colorCyan("║"))
	fmt.Println(colorCyan("╚════════════════════════════════════════════════════════╝"))
	fmt.Println()
}

func printHelp() {
	fmt.Println(colorCyan("\nAvailable Commands:"))
	fmt.Println("  /new              - Start a new conversation")
	fmt.Println("  /sessions         - List all conversation sessions")
	fmt.Println("  /switch <id>      - Switch to a specific session")
	fmt.Println("  /delete <id>      - Delete a session")
	fmt.Println("  /clear            - Clear current conversation history")
	fmt.Println("  /tools            - List available tools")
	fmt.Println("  /exec <command>   - Execute a shell command")
	fmt.Println("  /exit, /quit      - Exit the CLI")
	fmt.Println()
}

func showSessionInfo() {
	if currentSession == nil {
		return
	}
	info, err := sessionManager.GetSessionInfo(currentSession.ID)
	msgCount := 0
	if err == nil {
		msgCount = info.MessageCount
	}
	fmt.Printf("%s Session: %s | ID: %d | Messages: %d\n",
		colorDim("[INFO]"),
		colorWhite(truncate(currentSession.Title, 30)),
		currentSession.ID,
		msgCount)
	fmt.Println()
}

func newConversation() {
	session, err := sessionAgent.CreateSession(context.Background())
	if err != nil {
		fmt.Printf("%s Failed to create session: %v\n", colorRed("✗"), err)
		return
	}
	currentSession = session
	fmt.Printf("%s New conversation started. Session ID: %d\n", colorGreen("✓"), session.ID)
	fmt.Println()
}

func clearConversation() {
	if currentSession == nil {
		return
	}
	err := sessionManager.TruncateHistory(currentSession.ID, 0)
	if err != nil {
		fmt.Printf("%s Failed to clear conversation: %v\n", colorRed("✗"), err)
		return
	}
	fmt.Printf("%s Conversation history cleared.\n", colorGreen("✓"))
	fmt.Println()
}

func listSessions() {
	sessions, err := sessionManager.ListUserSessions(1)
	if err != nil {
		fmt.Printf("%s Failed to list sessions: %v\n", colorRed("✗"), err)
		return
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	fmt.Println(colorCyan("\n─── Sessions ───"))
	fmt.Printf("%-6s %-30s %-12s %s\n",
		colorDim("ID"),
		colorDim("Title"),
		colorDim("Status"),
		colorDim("Updated"))
	fmt.Println(colorDim("────────────────────────────────────────────────────────"))

	for _, s := range sessions {
		updated := s.UpdatedAt.Format("2006-01-02 15:04")
		marker := " "
		if currentSession != nil && s.ID == currentSession.ID {
			marker = colorGreen("*")
		}
		title := truncate(s.Title, 28)
		if title == "" {
			title = colorDim("<empty>")
		}
		fmt.Printf("%s%-6d %-30s %-12s %s\n",
			marker, s.ID, title, s.Status, colorDim(updated))
	}
	fmt.Println()
}

func switchSession(sessionIDStr string) {
	var sessionID uint
	_, err := fmt.Sscanf(sessionIDStr, "%d", &sessionID)
	if err != nil {
		fmt.Printf("%s Invalid session ID: %s\n", colorRed("✗"), sessionIDStr)
		return
	}

	session, err := sessionManager.GetSession(sessionID)
	if err != nil {
		fmt.Printf("%s Session not found: %d\n", colorRed("✗"), sessionID)
		return
	}

	currentSession = session
	showSessionInfo()
}

func deleteSession(sessionIDStr string) {
	var sessionID uint
	_, err := fmt.Sscanf(sessionIDStr, "%d", &sessionID)
	if err != nil {
		fmt.Printf("%s Invalid session ID: %s\n", colorRed("✗"), sessionIDStr)
		return
	}

	if currentSession != nil && sessionID == currentSession.ID {
		fmt.Printf("%s Cannot delete current session. Switch to another first.\n", colorRed("✗"))
		return
	}

	err = sessionManager.DeleteSession(sessionID)
	if err != nil {
		fmt.Printf("%s Failed to delete session: %v\n", colorRed("✗"), err)
		return
	}
	fmt.Printf("%s Session %d deleted.\n", colorGreen("✓"), sessionID)
}

func listTools(agentManager *agent.Manager) {
	registry := agentManager.GetToolRegistry()
	tools := registry.List()

	fmt.Println(colorCyan("\n─── Available Tools ───"))
	for _, tool := range tools {
		fmt.Printf("  %s%s%s: %s\n", colorGreen("•"), colorWhite(tool.Name), colorDim("()"), colorDim(tool.Description))
	}
	fmt.Println()
}

func execCommand(cmd string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shell := "cmd"
	shellArg := "/c"
	out, err := exec.CommandContext(ctx, shell, shellArg, cmd).CombinedOutput()
	if err != nil {
		fmt.Printf("%s Command failed: %v\n%s\n", colorRed("✗"), err, out)
		return
	}
	fmt.Printf("%s\n", string(out))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func colorRed(s string) string    { return "\033[31m" + s + "\033[0m" }
func colorGreen(s string) string  { return "\033[32m" + s + "\033[0m" }
func colorYellow(s string) string { return "\033[33m" + s + "\033[0m" }
func colorCyan(s string) string   { return "\033[36m" + s + "\033[0m" }
func colorWhite(s string) string  { return "\033[37m" + s + "\033[0m" }
func colorDim(s string) string    { return "\033[90m" + s + "\033[0m" }
