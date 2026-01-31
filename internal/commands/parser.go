package commands

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// CommandType represents the type of Lemuria command.
type CommandType string

const (
	CommandPlan     CommandType = "plan"
	CommandSync     CommandType = "sync"
	CommandUnlock   CommandType = "unlock"
	CommandHelp     CommandType = "help"
	CommandRollback CommandType = "rollback"
)

// Command represents a parsed Lemuria command.
type Command struct {
	Name        CommandType
	Application string
	All         bool
	Prune       bool
	DryRun      bool
	HistoryID   int64
	RawArgs     string
}

// commandPattern matches "lemuria <command>" with optional arguments.
var commandPattern = regexp.MustCompile(`(?i)^\s*lemuria\s+(\w+)(.*)$`)

// ErrNotCommand is returned when the input is not a Lemuria command.
var ErrNotCommand = errors.New("not a lemuria command")

// Parse extracts a Lemuria command from a comment body.
func Parse(body string) (*Command, error) {
	// Handle multi-line comments - find the lemuria command line
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if cmd, err := parseLine(line); err == nil {
			return cmd, nil
		}
	}

	return nil, ErrNotCommand
}

// parseLine parses a single line for a Lemuria command.
func parseLine(line string) (*Command, error) {
	matches := commandPattern.FindStringSubmatch(line)
	if matches == nil {
		return nil, ErrNotCommand
	}

	cmdName := strings.ToLower(matches[1])
	args := strings.TrimSpace(matches[2])

	cmd := &Command{
		RawArgs: args,
	}

	switch cmdName {
	case "plan":
		cmd.Name = CommandPlan
	case "sync":
		cmd.Name = CommandSync
	case "unlock":
		cmd.Name = CommandUnlock
	case "help":
		cmd.Name = CommandHelp
	case "rollback":
		cmd.Name = CommandRollback
	default:
		return nil, ErrNotCommand
	}

	// Parse arguments
	parseArgs(cmd, args)

	return cmd, nil
}

// parseArgs parses command arguments and flags.
func parseArgs(cmd *Command, args string) {
	if args == "" {
		return
	}

	tokens := tokenize(args)
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		switch token {
		case "-a", "--app", "--application":
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				cmd.Application = tokens[i+1]
				i++
			}
		case "--all":
			cmd.All = true
		case "--prune":
			cmd.Prune = true
		case "--dry-run":
			cmd.DryRun = true
		case "--id":
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				if id, err := strconv.ParseInt(tokens[i+1], 10, 64); err == nil {
					cmd.HistoryID = id
				}
				i++
			}
		default:
			// Treat bare words as application names if not a flag
			if !strings.HasPrefix(token, "-") && cmd.Application == "" {
				cmd.Application = token
			}
		}
	}
}

// tokenize splits an argument string into tokens, handling quotes.
func tokenize(args string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range args {
		switch {
		case r == '"' || r == '\'':
			if inQuote && r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else if !inQuote {
				inQuote = true
				quoteChar = r
			} else {
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// String returns a human-readable representation of the command.
func (c *Command) String() string {
	var parts []string
	parts = append(parts, "lemuria", string(c.Name))

	if c.Application != "" {
		parts = append(parts, "-a", c.Application)
	}
	if c.All {
		parts = append(parts, "--all")
	}
	if c.Prune {
		parts = append(parts, "--prune")
	}
	if c.DryRun {
		parts = append(parts, "--dry-run")
	}
	if c.HistoryID > 0 {
		parts = append(parts, "--id", strconv.FormatInt(c.HistoryID, 10))
	}

	return strings.Join(parts, " ")
}
