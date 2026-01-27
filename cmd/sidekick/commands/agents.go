package commands

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/db"
	"github.com/earlysvahn/sidekick/internal/sync"
)

// RunAgentsCommand handles the 'agents' subcommand
func RunAgentsCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agents command requires a subcommand: list, show, create, update, delete, enable, disable")
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "list":
		return runAgentsListCommand()
	case "show":
		return runAgentsShowCommand(subArgs)
	case "create":
		return runAgentsCreateCommand(subArgs)
	case "update":
		return runAgentsUpdateCommand(subArgs)
	case "delete":
		return runAgentsDeleteCommand(subArgs)
	case "enable":
		return runAgentsEnableCommand(subArgs)
	case "disable":
		return runAgentsDisableCommand(subArgs)
	default:
		return fmt.Errorf("unknown agents subcommand: %s", subcommand)
	}
}

// runAgentsListCommand lists all agents
func runAgentsListCommand() error {
	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)
	agents, err := repo.List()
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	fmt.Printf("%-20s %-20s %-8s %-10s %-20s %-8s\n", "ID", "NAME", "ENABLED", "VERBOSITY", "MODEL", "REVISION")
	for _, a := range agents {
		enabled := "false"
		if a.Enabled {
			enabled = "true"
		}
		fmt.Printf("%-20s %-20s %-8s %-10d %-20s %-8d\n", a.ID, a.Name, enabled, a.DefaultVerbosity, a.Model, a.Revision)
	}

	return nil
}

// runAgentsShowCommand shows full agent details
func runAgentsShowCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("show requires agent id")
	}

	agentID := args[0]

	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)
	a, err := repo.Get(agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if a == nil {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	output := map[string]interface{}{
		"id":                a.ID,
		"name":              a.Name,
		"base_agent":        a.BaseAgent,
		"model":             a.Model,
		"system_prompt":     a.SystemPrompt,
		"default_verbosity": a.DefaultVerbosity,
		"enabled":           a.Enabled,
		"revision":          a.Revision,
		"updated_at":        a.UpdatedAt.Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// runAgentsCreateCommand creates a new agent
func runAgentsCreateCommand(args []string) error {
	fs := flag.NewFlagSet("agents create", flag.ExitOnError)
	var filePath string
	fs.StringVar(&filePath, "file", "", "path to JSON file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var data []byte
	var err error

	if filePath != "" {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	var input struct {
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		BaseAgent        *string `json:"base_agent"`
		Model            string  `json:"model"`
		SystemPrompt     string  `json:"system_prompt"`
		DefaultVerbosity int     `json:"default_verbosity"`
		Enabled          bool    `json:"enabled"`
	}

	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)

	existing, err := repo.Get(input.ID)
	if err != nil {
		return fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("agent already exists: %s", input.ID)
	}

	newAgent := &agent.AgentRecord{
		ID:               input.ID,
		Name:             input.Name,
		BaseAgent:        input.BaseAgent,
		Model:            input.Model,
		SystemPrompt:     input.SystemPrompt,
		DefaultVerbosity: input.DefaultVerbosity,
		Enabled:          input.Enabled,
	}

	if err := repo.Create(newAgent); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	fmt.Printf("Agent created: %s\n", input.ID)

	if err := syncAgentsToPostgres(database); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: postgres sync failed: %v\n", err)
	}

	return nil
}

// runAgentsUpdateCommand updates an existing agent
func runAgentsUpdateCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("update requires agent id")
	}

	agentID := args[0]

	fs := flag.NewFlagSet("agents update", flag.ExitOnError)
	var filePath string
	fs.StringVar(&filePath, "file", "", "path to JSON file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	var data []byte
	var err error

	if filePath != "" {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	var input map[string]interface{}
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)

	existing, err := repo.Get(agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	if name, ok := input["name"].(string); ok {
		existing.Name = name
	}
	if model, ok := input["model"].(string); ok {
		existing.Model = model
	}
	if prompt, ok := input["system_prompt"].(string); ok {
		existing.SystemPrompt = prompt
	}
	if verbosity, ok := input["default_verbosity"].(float64); ok {
		existing.DefaultVerbosity = int(verbosity)
	}
	if enabled, ok := input["enabled"].(bool); ok {
		existing.Enabled = enabled
	}
	if baseAgent, ok := input["base_agent"]; ok {
		if baseAgent == nil {
			existing.BaseAgent = nil
		} else if ba, ok := baseAgent.(string); ok {
			existing.BaseAgent = &ba
		}
	}

	if err := repo.Update(existing); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	fmt.Printf("Agent updated: %s\n", agentID)

	if err := syncAgentsToPostgres(database); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: postgres sync failed: %v\n", err)
	}

	return nil
}

// runAgentsDeleteCommand deletes an agent
func runAgentsDeleteCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("delete requires agent id")
	}

	agentID := args[0]

	if agentID == "default" {
		return fmt.Errorf("cannot delete default agent")
	}

	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)

	if err := repo.Delete(agentID); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	fmt.Printf("Agent deleted: %s\n", agentID)

	if err := syncAgentsToPostgres(database); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: postgres sync failed: %v\n", err)
	}

	return nil
}

// runAgentsEnableCommand enables an agent
func runAgentsEnableCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("enable requires agent id")
	}

	return setAgentEnabled(args[0], true)
}

// runAgentsDisableCommand disables an agent
func runAgentsDisableCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("disable requires agent id")
	}

	return setAgentEnabled(args[0], false)
}

// setAgentEnabled sets the enabled flag for an agent
func setAgentEnabled(agentID string, enabled bool) error {
	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	repo := agent.NewRepository(database)

	existing, err := repo.Get(agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	existing.Enabled = enabled

	if err := repo.Update(existing); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	status := "disabled"
	if enabled {
		status = "enabled"
	}
	fmt.Printf("Agent %s: %s\n", status, agentID)

	if err := syncAgentsToPostgres(database); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: postgres sync failed: %v\n", err)
	}

	return nil
}

// syncAgentsToPostgres triggers postgres sync if DSN is configured
func syncAgentsToPostgres(sqliteDB *sql.DB) error {
	return sync.AutoSyncAgents(sqliteDB)
}
