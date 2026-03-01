package cli

import (
	"testing"
)

func TestParseFieldValue(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"hello", "hello"},
		{"42", float64(42)},
		{"true", true},
		{"false", false},
		{"null", nil},
		{`[1,2,3]`, []any{float64(1), float64(2), float64(3)}},
		{`{"a":1}`, map[string]any{"a": float64(1)}},
		{`"quoted"`, "quoted"},
		{"not json at all {{", "not json at all {{"},
	}

	for _, tt := range tests {
		got := parseFieldValue(tt.input)
		// Compare via sprintf for simplicity.
		gotStr := formatTestValue(got)
		wantStr := formatTestValue(tt.want)
		if gotStr != wantStr {
			t.Errorf("parseFieldValue(%q) = %v, want %v", tt.input, gotStr, wantStr)
		}
	}
}

func formatTestValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	return formatValue(v)
}

func TestParseShortFlags(t *testing.T) {
	args := []string{"/v1/tables", "-X", "POST", "-F", "name=test", "-H", "X-Custom: value"}
	pos, flags, err := parseFlags(args)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(pos) != 1 || pos[0] != "/v1/tables" {
		t.Errorf("positional = %v, want [\"/v1/tables\"]", pos)
	}
	if flags["X"][0] != "POST" {
		t.Errorf("X = %v, want [\"POST\"]", flags["X"])
	}
	if flags["F"][0] != "name=test" {
		t.Errorf("F = %v, want [\"name=test\"]", flags["F"])
	}
	if flags["H"][0] != "X-Custom: value" {
		t.Errorf("H = %v, want [\"X-Custom: value\"]", flags["H"])
	}
}
