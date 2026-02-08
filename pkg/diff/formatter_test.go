package diff

import (
	"strings"
	"testing"
)

func TestFormatterFormatResourceList(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name      string
		resources []string
		want      string
	}{
		{
			name:      "empty list",
			resources: nil,
			want:      "_None_",
		},
		{
			name:      "single resource",
			resources: []string{"Deployment/my-app"},
			want:      "- `Deployment/my-app`\n",
		},
		{
			name:      "multiple resources",
			resources: []string{"Deployment/app", "Service/app", "ConfigMap/config"},
			want:      "- `Deployment/app`\n- `Service/app`\n- `ConfigMap/config`\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.FormatResourceList(tt.resources); got != tt.want {
				t.Errorf("FormatResourceList() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatterFormatCodeBlock(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name     string
		content  string
		language string
		want     string
	}{
		{
			name:     "yaml block",
			content:  "key: value",
			language: "yaml",
			want:     "```yaml\nkey: value\n```",
		},
		{
			name:     "diff block",
			content:  "+added\n-removed",
			language: "diff",
			want:     "```diff\n+added\n-removed\n```",
		},
		{
			name:     "empty language",
			content:  "some text",
			language: "",
			want:     "```\nsome text\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.FormatCodeBlock(tt.content, tt.language); got != tt.want {
				t.Errorf("FormatCodeBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatterFormatCollapsible(t *testing.T) {
	f := NewFormatter()

	got := f.FormatCollapsible("Click to expand", "hidden content")
	if !strings.Contains(got, "<details>") {
		t.Error("FormatCollapsible() missing <details> tag")
	}
	if !strings.Contains(got, "<summary>Click to expand</summary>") {
		t.Error("FormatCollapsible() missing summary")
	}
	if !strings.Contains(got, "hidden content") {
		t.Error("FormatCollapsible() missing content")
	}
}

func TestFormatterFormatTable(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{
			name:    "empty headers",
			headers: nil,
			rows:    [][]string{{"a"}},
			want:    "",
		},
		{
			name:    "empty rows",
			headers: []string{"Name"},
			rows:    nil,
			want:    "",
		},
		{
			name:    "simple table",
			headers: []string{"Name", "Status"},
			rows:    [][]string{{"app", "synced"}, {"db", "out-of-sync"}},
			want:    "| Name | Status |\n|---|---|\n| app | synced |\n| db | out-of-sync |\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.FormatTable(tt.headers, tt.rows); got != tt.want {
				t.Errorf("FormatTable() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatterFormatDuration(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name    string
		seconds int
		want    string
	}{
		{"zero", 0, "0s"},
		{"seconds only", 45, "45s"},
		{"exactly one minute", 60, "1m 0s"},
		{"minutes and seconds", 125, "2m 5s"},
		{"one hour", 3600, "1h 0m"},
		{"hours and minutes", 3665, "1h 1m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.FormatDuration(tt.seconds); got != tt.want {
				t.Errorf("FormatDuration(%d) = %q, want %q", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestFormatterTruncateString(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 8, "hello..."},
		{"very short max", "hello", 2, "he"},
		{"max equals 3", "hello", 3, "hel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.TruncateString(tt.s, tt.maxLen); got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestFormatterIndentLines(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name   string
		s      string
		spaces int
		want   string
	}{
		{"single line", "hello", 2, "  hello"},
		{"multiple lines", "line1\nline2", 4, "    line1\n    line2"},
		{"empty lines preserved", "line1\n\nline2", 2, "  line1\n\n  line2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.IndentLines(tt.s, tt.spaces); got != tt.want {
				t.Errorf("IndentLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatterStripANSI(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name string
		s    string
		want string
	}{
		{"no ANSI", "hello", "hello"},
		{"with color codes", "\x1b[31mred\x1b[0m text", "red text"},
		{"multiple codes", "\x1b[1m\x1b[32mbold green\x1b[0m", "bold green"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.StripANSI(tt.s); got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

func TestFormatterWrapInQuotes(t *testing.T) {
	f := NewFormatter()

	if got := f.WrapInQuotes("hello"); got != "`hello`" {
		t.Errorf("WrapInQuotes() = %q, want %q", got, "`hello`")
	}
}

func TestFormatterJoinWithCommas(t *testing.T) {
	f := NewFormatter()

	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{"empty", nil, ""},
		{"one item", []string{"apple"}, "apple"},
		{"two items", []string{"apple", "banana"}, "apple and banana"},
		{"three items", []string{"apple", "banana", "cherry"}, "apple, banana, and cherry"},
		{"four items", []string{"a", "b", "c", "d"}, "a, b, c, and d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.JoinWithCommas(tt.items); got != tt.want {
				t.Errorf("JoinWithCommas() = %q, want %q", got, tt.want)
			}
		})
	}
}
