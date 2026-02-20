package editor

import "testing"

func TestLineCountEmpty(t *testing.T) {
	if got := LineCount(""); got != 1 {
		t.Errorf("LineCount(\"\") = %d, want 1", got)
	}
}

func TestLineCountSingleLine(t *testing.T) {
	if got := LineCount("hello"); got != 1 {
		t.Errorf("LineCount(\"hello\") = %d, want 1", got)
	}
}

func TestLineCountMultipleLines(t *testing.T) {
	if got := LineCount("a\nb\nc"); got != 3 {
		t.Errorf("LineCount(\"a\\nb\\nc\") = %d, want 3", got)
	}
}

func TestLineCountTrailingNewline(t *testing.T) {
	if got := LineCount("a\nb\n"); got != 3 {
		t.Errorf("LineCount(\"a\\nb\\n\") = %d, want 3", got)
	}
}

func TestDeleteLineSingleLine(t *testing.T) {
	got := DeleteLine("hello", 0)
	if got != "" {
		t.Errorf("DeleteLine single line = %q, want %q", got, "")
	}
}

func TestDeleteLineFirstOfTwo(t *testing.T) {
	got := DeleteLine("first\nsecond", 0)
	want := "second"
	if got != want {
		t.Errorf("DeleteLine first of two = %q, want %q", got, want)
	}
}

func TestDeleteLineLastOfTwo(t *testing.T) {
	got := DeleteLine("first\nsecond", 1)
	want := "first"
	if got != want {
		t.Errorf("DeleteLine last of two = %q, want %q", got, want)
	}
}

func TestDeleteLineMiddle(t *testing.T) {
	got := DeleteLine("aaa\nbbb\nccc", 1)
	want := "aaa\nccc"
	if got != want {
		t.Errorf("DeleteLine middle = %q, want %q", got, want)
	}
}

func TestDeleteLineEmptyText(t *testing.T) {
	got := DeleteLine("", 0)
	if got != "" {
		t.Errorf("DeleteLine empty = %q, want %q", got, "")
	}
}

func TestDeleteLineOutOfRange(t *testing.T) {
	text := "a\nb"
	if got := DeleteLine(text, -1); got != text {
		t.Errorf("DeleteLine negative index = %q, want %q", got, text)
	}
	if got := DeleteLine(text, 5); got != text {
		t.Errorf("DeleteLine too large = %q, want %q", got, text)
	}
}

func TestMoveLineDown(t *testing.T) {
	got := MoveLine("aaa\nbbb\nccc", 0, 1)
	want := "bbb\naaa\nccc"
	if got != want {
		t.Errorf("MoveLine down = %q, want %q", got, want)
	}
}

func TestMoveLineUp(t *testing.T) {
	got := MoveLine("aaa\nbbb\nccc", 2, -1)
	want := "aaa\nccc\nbbb"
	if got != want {
		t.Errorf("MoveLine up = %q, want %q", got, want)
	}
}

func TestMoveLineFirstUp(t *testing.T) {
	text := "aaa\nbbb"
	got := MoveLine(text, 0, -1)
	if got != text {
		t.Errorf("MoveLine first up = %q, want unchanged %q", got, text)
	}
}

func TestMoveLineLastDown(t *testing.T) {
	text := "aaa\nbbb"
	got := MoveLine(text, 1, 1)
	if got != text {
		t.Errorf("MoveLine last down = %q, want unchanged %q", got, text)
	}
}

func TestMoveLineMiddle(t *testing.T) {
	got := MoveLine("aaa\nbbb\nccc", 1, 1)
	want := "aaa\nccc\nbbb"
	if got != want {
		t.Errorf("MoveLine middle down = %q, want %q", got, want)
	}
}

func TestMoveLineOutOfRange(t *testing.T) {
	text := "a\nb"
	if got := MoveLine(text, -1, 1); got != text {
		t.Errorf("MoveLine negative = %q, want %q", got, text)
	}
	if got := MoveLine(text, 5, 1); got != text {
		t.Errorf("MoveLine too large = %q, want %q", got, text)
	}
}

func TestDuplicateLineFirst(t *testing.T) {
	got := DuplicateLine("aaa\nbbb\nccc", 0)
	want := "aaa\naaa\nbbb\nccc"
	if got != want {
		t.Errorf("DuplicateLine first = %q, want %q", got, want)
	}
}

func TestDuplicateLineMiddle(t *testing.T) {
	got := DuplicateLine("aaa\nbbb\nccc", 1)
	want := "aaa\nbbb\nbbb\nccc"
	if got != want {
		t.Errorf("DuplicateLine middle = %q, want %q", got, want)
	}
}

func TestDuplicateLineLast(t *testing.T) {
	got := DuplicateLine("aaa\nbbb\nccc", 2)
	want := "aaa\nbbb\nccc\nccc"
	if got != want {
		t.Errorf("DuplicateLine last = %q, want %q", got, want)
	}
}

func TestDuplicateLineSingle(t *testing.T) {
	got := DuplicateLine("only", 0)
	want := "only\nonly"
	if got != want {
		t.Errorf("DuplicateLine single = %q, want %q", got, want)
	}
}

func TestDuplicateLineOutOfRange(t *testing.T) {
	text := "a\nb"
	if got := DuplicateLine(text, -1); got != text {
		t.Errorf("DuplicateLine negative = %q, want %q", got, text)
	}
	if got := DuplicateLine(text, 5); got != text {
		t.Errorf("DuplicateLine too large = %q, want %q", got, text)
	}
}
