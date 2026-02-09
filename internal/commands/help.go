// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"context"

	"github.com/org/lemuria/internal/models"
)

// executeHelp posts help information.
func (e *Executor) executeHelp(ctx context.Context, event *models.PREvent) error {
	helpText := `## Lemuria Help

Lemuria provides Argo CD pull request automation.

### Commands

| Command | Description |
|---------|-------------|
| ` + "`lemuria plan`" + ` | Generate diff of changes for affected applications |
| ` + "`lemuria plan -a <app>`" + ` | Plan specific application |
| ` + "`lemuria plan --all`" + ` | Plan all applications (ignore path filtering) |
| ` + "`lemuria sync`" + ` | Sync all planned applications |
| ` + "`lemuria sync -a <app>`" + ` | Sync specific application |
| ` + "`lemuria sync --prune`" + ` | Sync with prune enabled |
| ` + "`lemuria sync --dry-run`" + ` | Dry-run sync only |
| ` + "`lemuria rollback`" + ` | Rollback all locked apps to their targetRevision |
| ` + "`lemuria rollback -a <app>`" + ` | Rollback specific app to its targetRevision |
| ` + "`lemuria rollback --dry-run`" + ` | Preview rollback |
| ` + "`lemuria unlock`" + ` | Release all locks for this PR |
| ` + "`lemuria unlock -a <app>`" + ` | Release lock for specific app |
| ` + "`lemuria help`" + ` | Show this help message |

### Workflow

1. **Plan**: When a PR is opened or updated, Lemuria automatically runs ` + "`lemuria plan`" + ` to show what would change.
2. **Review**: Review the diff in the PR comment.
3. **Sync**: Comment ` + "`lemuria sync`" + ` to apply the changes.
4. **Unlock**: If you need to abandon changes, comment ` + "`lemuria unlock`" + `.

### Rollback

To rollback applications after a PR sync:
- Run ` + "`lemuria rollback`" + ` to rollback all apps locked by this PR
- Run ` + "`lemuria rollback -a <app>`" + ` to rollback a specific app
- This syncs apps back to their configured targetRevision (main/master)

### Locking

- Applications are locked when planned to prevent concurrent modifications.
- Only the PR that holds the lock can sync the application.
- Locks are automatically released when the PR is closed or merged.

### Requirements

Depending on configuration, sync and rollback may require:
- PR approval
- No merge conflicts
- Valid (non-stale) plan (for sync)

---
[Lemuria Documentation](https://github.com/pradithya/lemuria)`

	return e.postComment(ctx, event, "", helpText)
}
