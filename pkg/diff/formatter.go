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

package diff

import (
	"fmt"
	"strings"
)

// Formatter provides utility functions for formatting output.
type Formatter struct{}

// NewFormatter creates a new output formatter.
func NewFormatter() *Formatter {
	return &Formatter{}
}

// FormatResourceList formats a list of resources as a bulleted list.
func (f *Formatter) FormatResourceList(resources []string) string {
	if len(resources) == 0 {
		return "_None_"
	}

	var sb strings.Builder
	for _, r := range resources {
		sb.WriteString(fmt.Sprintf("- `%s`\n", r))
	}
	return sb.String()
}

// FormatCodeBlock wraps content in a markdown code block.
func (f *Formatter) FormatCodeBlock(content, language string) string {
	return fmt.Sprintf("```%s\n%s\n```", language, content)
}

// FormatCollapsible wraps content in a collapsible details section.
func (f *Formatter) FormatCollapsible(summary, content string) string {
	return fmt.Sprintf("<details>\n<summary>%s</summary>\n\n%s\n</details>", summary, content)
}

// FormatTable formats data as a markdown table.
func (f *Formatter) FormatTable(headers []string, rows [][]string) string {
	if len(headers) == 0 || len(rows) == 0 {
		return ""
	}

	var sb strings.Builder

	// Header row
	sb.WriteString("| ")
	sb.WriteString(strings.Join(headers, " | "))
	sb.WriteString(" |\n")

	// Separator row
	sb.WriteString("|")
	for range headers {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")

	// Data rows
	for _, row := range rows {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(row, " | "))
		sb.WriteString(" |\n")
	}

	return sb.String()
}

// FormatDuration formats a duration in a human-readable way.
func (f *Formatter) FormatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// TruncateString truncates a string to the specified length, adding ellipsis if needed.
func (f *Formatter) TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// IndentLines indents each line of the string by the specified number of spaces.
func (f *Formatter) IndentLines(s string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// StripANSI removes ANSI escape codes from a string.
func (f *Formatter) StripANSI(s string) string {
	var result strings.Builder
	inEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}

	return result.String()
}

// WrapInQuotes wraps a string in backticks for inline code formatting.
func (f *Formatter) WrapInQuotes(s string) string {
	return "`" + s + "`"
}

// JoinWithCommas joins strings with commas and "and" for the last item.
func (f *Formatter) JoinWithCommas(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}
