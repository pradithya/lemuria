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

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/org/lemuria/internal/commands"
)

func TestE2EHelpCommand(t *testing.T) {
	mockGH := NewMockVCSClient()
	executor := newTestExecutor(mockGH, nil)

	event := newPREvent("test-owner/test-repo", "test-owner", "test-repo", 900, "", "", "", "lemuria help")
	cmd := &commands.Command{
		Name: commands.CommandHelp,
	}

	// Use a background context for this simple test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := executor.Execute(ctx, cmd, event)
	if err != nil {
		t.Fatalf("Help command failed: %v", err)
	}

	// Assert: help comment posted
	comments := mockGH.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected help comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}

func TestE2EHelpCommandGitLab(t *testing.T) {
	mockVCS := NewMockVCSClient()
	executor := newTestExecutor(mockVCS, nil)

	event := newGitLabPREvent("mygroup/myproject", "mygroup", "myproject", 900, "", "", "", "lemuria help")
	cmd := &commands.Command{
		Name: commands.CommandHelp,
	}

	// Use a background context for this simple test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := executor.Execute(ctx, cmd, event)
	if err != nil {
		t.Fatalf("Help command failed: %v", err)
	}

	// Assert: help comment posted
	comments := mockVCS.GetPostedComments()
	if len(comments) == 0 {
		t.Fatal("Expected help comment to be posted")
	}

	lastComment := comments[len(comments)-1]
	if !strings.Contains(lastComment.Body, "Help") {
		t.Error("Expected 'Help' in comment body")
	}
	if !strings.Contains(lastComment.Body, "lemuria plan") {
		t.Error("Expected command examples in help text")
	}
}
