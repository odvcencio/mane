package editor

// Selection represents a text selection as two byte offsets into buffer text.
// Anchor is where the selection started, Cursor is where it currently extends to.
type Selection struct {
	Anchor, Cursor int
}

// Active reports whether the selection covers a non-empty range.
func (s *Selection) Active() bool {
	return s.Anchor != s.Cursor
}

// Ordered returns the selection bounds in ascending order (start, end).
func (s *Selection) Ordered() (start, end int) {
	if s.Anchor <= s.Cursor {
		return s.Anchor, s.Cursor
	}
	return s.Cursor, s.Anchor
}

// Text extracts the selected substring from content.
func (s *Selection) Text(content string) string {
	start, end := s.Ordered()
	if start < 0 {
		start = 0
	}
	if end > len(content) {
		end = len(content)
	}
	if start >= end {
		return ""
	}
	return content[start:end]
}

// Clear collapses the selection so that Anchor equals Cursor.
func (s *Selection) Clear() {
	s.Anchor = s.Cursor
}

// SelectAll expands the selection to cover the entire content of the given length.
func (s *Selection) SelectAll(length int) {
	s.Anchor = 0
	s.Cursor = length
}
