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

package gitlab

import (
	"strings"
	"testing"
)

func TestBuildStaleBody(t *testing.T) {
	body := CommentMarker + "\n" + PlanCommentMarker + "\n" +
		"## Lemuria Plan\n\n" +
		"### Application: `my-app`\n\n" +
		"Some diff content\n"

	result := buildStaleBody(body)

	// Should contain stale marker
	if !strings.Contains(result, StaleMarker) {
		t.Error("expected stale marker in output")
	}

	// Should contain stale notice
	if !strings.Contains(result, StaleNotice) {
		t.Error("expected stale notice in output")
	}

	// Should wrap content in collapsible details block
	if !strings.Contains(result, "<details>") {
		t.Error("expected <details> tag in output")
	}
	if !strings.Contains(result, "<summary>Show outdated plan</summary>") {
		t.Error("expected collapsible summary in output")
	}
	if !strings.Contains(result, "</details>") {
		t.Error("expected closing </details> tag in output")
	}

	// The plan content should be inside the details block
	detailsStart := strings.Index(result, "<details>")
	detailsEnd := strings.Index(result, "</details>")
	planIdx := strings.Index(result, "## Lemuria Plan")
	if planIdx < detailsStart || planIdx > detailsEnd {
		t.Error("expected plan content to be inside <details> block")
	}

	// Stale notice should be before the details block
	noticeIdx := strings.Index(result, StaleNotice)
	if noticeIdx > detailsStart {
		t.Error("expected stale notice to be before <details> block")
	}
}

func TestBuildStaleBodyNoHeading(t *testing.T) {
	body := CommentMarker + "\n" + PlanCommentMarker + "\n" +
		"Some content without headings\n"

	result := buildStaleBody(body)

	// Should still contain stale marker
	if !strings.Contains(result, StaleMarker) {
		t.Error("expected stale marker in output")
	}

	// Without a heading, content is not wrapped in details
	if strings.Contains(result, "<details>") {
		t.Error("expected no <details> tag when no heading found")
	}
}
