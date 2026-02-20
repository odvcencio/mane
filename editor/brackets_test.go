package editor

import "testing"

func TestFindMatchingBracket(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		pos     int
		wantPos int
		wantOK  bool
	}{
		{
			name:    "open paren",
			text:    "a(b)",
			pos:     1,
			wantPos: 3,
			wantOK:  true,
		},
		{
			name:    "close paren",
			text:    "a(b)",
			pos:     3,
			wantPos: 1,
			wantOK:  true,
		},
		{
			name:    "nested",
			text:    "((a))",
			pos:     0,
			wantPos: 4,
			wantOK:  true,
		},
		{
			name:    "open brace",
			text:    "{x}",
			pos:     0,
			wantPos: 2,
			wantOK:  true,
		},
		{
			name:    "open bracket",
			text:    "[x]",
			pos:     0,
			wantPos: 2,
			wantOK:  true,
		},
		{
			name:    "no match",
			text:    "a(b",
			pos:     1,
			wantPos: 0,
			wantOK:  false,
		},
		{
			name:    "not a bracket",
			text:    "abc",
			pos:     1,
			wantPos: 0,
			wantOK:  false,
		},
		{
			name:    "empty text",
			text:    "",
			pos:     0,
			wantPos: 0,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPos, gotOK := FindMatchingBracket(tt.text, tt.pos)
			if gotPos != tt.wantPos || gotOK != tt.wantOK {
				t.Errorf("FindMatchingBracket(%q, %d) = (%d, %v), want (%d, %v)",
					tt.text, tt.pos, gotPos, gotOK, tt.wantPos, tt.wantOK)
			}
		})
	}
}
