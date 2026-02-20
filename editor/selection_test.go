package editor

import "testing"

func TestSelectionActive(t *testing.T) {
	s := Selection{Anchor: 0, Cursor: 0}
	if s.Active() {
		t.Error("empty selection should not be active")
	}

	s.Cursor = 5
	if !s.Active() {
		t.Error("selection with different anchor and cursor should be active")
	}
}

func TestSelectionOrdered(t *testing.T) {
	// Forward selection
	s := Selection{Anchor: 2, Cursor: 8}
	start, end := s.Ordered()
	if start != 2 || end != 8 {
		t.Errorf("Ordered() = (%d, %d), want (2, 8)", start, end)
	}

	// Backward selection
	s = Selection{Anchor: 10, Cursor: 3}
	start, end = s.Ordered()
	if start != 3 || end != 10 {
		t.Errorf("Ordered() = (%d, %d), want (3, 10)", start, end)
	}

	// Empty selection
	s = Selection{Anchor: 5, Cursor: 5}
	start, end = s.Ordered()
	if start != 5 || end != 5 {
		t.Errorf("Ordered() = (%d, %d), want (5, 5)", start, end)
	}
}

func TestSelectionText(t *testing.T) {
	content := "hello, world!"

	// Forward selection
	s := Selection{Anchor: 0, Cursor: 5}
	got := s.Text(content)
	if got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}

	// Backward selection
	s = Selection{Anchor: 13, Cursor: 7}
	got = s.Text(content)
	if got != "world!" {
		t.Errorf("Text() = %q, want %q", got, "world!")
	}

	// Empty selection
	s = Selection{Anchor: 3, Cursor: 3}
	got = s.Text(content)
	if got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}

	// Full content
	s = Selection{Anchor: 0, Cursor: len(content)}
	got = s.Text(content)
	if got != content {
		t.Errorf("Text() = %q, want %q", got, content)
	}
}

func TestSelectionClear(t *testing.T) {
	s := Selection{Anchor: 2, Cursor: 10}
	s.Clear()
	if s.Active() {
		t.Error("selection should not be active after Clear")
	}
	if s.Anchor != s.Cursor {
		t.Errorf("after Clear, Anchor=%d Cursor=%d, want equal", s.Anchor, s.Cursor)
	}
	// Clear sets Anchor = Cursor, so both should be at Cursor's position
	if s.Anchor != 10 {
		t.Errorf("after Clear, Anchor=%d, want 10", s.Anchor)
	}
}

func TestSelectionSelectAll(t *testing.T) {
	s := Selection{Anchor: 3, Cursor: 3}
	s.SelectAll(42)
	if s.Anchor != 0 {
		t.Errorf("after SelectAll, Anchor=%d, want 0", s.Anchor)
	}
	if s.Cursor != 42 {
		t.Errorf("after SelectAll, Cursor=%d, want 42", s.Cursor)
	}
	if !s.Active() {
		t.Error("selection should be active after SelectAll with non-zero length")
	}
}

func TestSelectionSelectAllEmpty(t *testing.T) {
	s := Selection{Anchor: 5, Cursor: 10}
	s.SelectAll(0)
	if s.Anchor != 0 || s.Cursor != 0 {
		t.Errorf("after SelectAll(0), Anchor=%d Cursor=%d, want both 0", s.Anchor, s.Cursor)
	}
	if s.Active() {
		t.Error("selection should not be active after SelectAll(0)")
	}
}

func TestSelectionTextBoundsClamp(t *testing.T) {
	content := "short"

	// Selection beyond content length should be clamped
	s := Selection{Anchor: 0, Cursor: 100}
	got := s.Text(content)
	if got != "short" {
		t.Errorf("Text() = %q, want %q", got, "short")
	}

	// Negative anchor should be clamped to 0
	s = Selection{Anchor: -5, Cursor: 3}
	got = s.Text(content)
	if got != "sho" {
		t.Errorf("Text() = %q, want %q", got, "sho")
	}
}
