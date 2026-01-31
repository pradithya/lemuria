package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// executeUnlock runs the unlock command.
func (e *Executor) executeUnlock(ctx context.Context, cmd *Command, event *models.PREvent) error {
	// Add reaction to show we're working on it
	if event.Comment != nil {
		e.github.AddReaction(ctx, event.Repo.Owner, event.Repo.Name, event.Comment.ID, "eyes")
	}

	// Get locks held by this PR
	locks, err := e.lock.ListByPR(ctx, event.Repo.FullName, event.PR.Number)
	if err != nil {
		return e.postError(ctx, event, fmt.Errorf("listing locks: %w", err))
	}

	if len(locks) == 0 {
		return e.postComment(ctx, event, "", "## Lemuria Unlock\n\nNo applications are locked by this PR.")
	}

	// Filter to specific application if requested
	if cmd.Application != "" {
		var filtered []models.Lock
		for _, l := range locks {
			if l.Application == cmd.Application {
				filtered = append(filtered, l)
			}
		}
		if len(filtered) == 0 {
			return e.postComment(ctx, event, "", fmt.Sprintf("## Lemuria Unlock\n\n⚠️ Application `%s` is not locked by this PR.", cmd.Application))
		}
		locks = filtered
	}

	// Unlock each application
	var results []unlockResult
	for _, l := range locks {
		result := unlockResult{
			Application: l.Application,
		}

		if err := e.lock.Unlock(ctx, l.Application, event.Repo.FullName, event.PR.Number); err != nil {
			result.Error = err
		} else {
			result.Success = true
		}

		results = append(results, result)
	}

	// Render and post results
	output := e.renderUnlockResults(results)
	return e.postComment(ctx, event, "", output)
}

// unlockResult holds the result of unlocking a single application.
type unlockResult struct {
	Application string
	Success     bool
	Error       error
}

// renderUnlockResults formats unlock results as a markdown comment.
func (e *Executor) renderUnlockResults(results []unlockResult) string {
	var sb strings.Builder
	sb.WriteString("## Lemuria Unlock\n\n")

	allSucceeded := true
	for _, r := range results {
		if r.Error != nil {
			sb.WriteString(fmt.Sprintf("❌ `%s`: %s\n", r.Application, r.Error.Error()))
			allSucceeded = false
		} else {
			sb.WriteString(fmt.Sprintf("✅ `%s`: Unlocked\n", r.Application))
		}
	}

	if allSucceeded && len(results) > 0 {
		sb.WriteString("\n---\n")
		sb.WriteString("🔓 All locks released. Plans discarded.\n")
	}

	return sb.String()
}
