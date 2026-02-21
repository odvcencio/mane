package editor

import "testing"

func TestComputeIndent(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"no indent", "hello", ""},
		{"preserve tab indent", "\thello", "\t"},
		{"preserve space indent", "    hello", "    "},
		{"increase after brace", "\tif x {", "\t\t"},
		{"increase after paren with spaces", "    func(", "        "},
		{"increase after colon with spaces", "    if condition:", "        "},
		{"increase after tabbed colon", "\tif condition:", "\t\t"},
		{"empty line", "", ""},
		{"only whitespace", "    ", "    "},
		{"increase after bracket", "items [", "\t"},
		{"tab indent increase after bracket", "\tarr = [", "\t\t"},
		{"no increase without bracket", "\treturn 1", "\t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeIndent(tt.line)
			if got != tt.want {
				t.Errorf("ComputeIndent(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestDetectIndentStyle(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"tabs", "func main() {\n\tfmt.Println()\n}", "\t"},
		{"spaces", "def main():\n    print()\n", "    "},
		{"no indent defaults to tab", "a\nb\nc\n", "\t"},
		{"empty text defaults to tab", "", "\t"},
		{"two space indent", "if true:\n  pass\n", "  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIndentStyle(tt.text)
			if got != tt.want {
				t.Errorf("DetectIndentStyle(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
