package editor

import "testing"

func TestNewMultiCursor(t *testing.T) {
	mc := NewMultiCursor()
	if mc.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", mc.Count())
	}
	primary := mc.Primary()
	if primary.Offset != 0 || primary.Anchor != 0 {
		t.Fatalf("Primary() = %+v, want {0 0}", primary)
	}
}

func TestMultiCursorInsertAtAll(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(0, 3)
	mc.AddSelection(4, 7)
	got := mc.InsertAtAll("abc\nabc", "X")
	want := "X\nX"
	if got != want {
		t.Fatalf("InsertAtAll() = %q, want %q", got, want)
	}
	cursors := mc.Cursors()
	if len(cursors) != 2 {
		t.Fatalf("cursor count = %d, want 2", len(cursors))
	}
	if cursors[0].Offset != 1 || cursors[1].Offset != 3 {
		t.Fatalf("unexpected cursor offsets after edit: %+v", cursors)
	}
}

func TestMultiCursorInsertAtAllInsertAndReplaceOffsets(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(1, 1)
	mc.AddCursor(3)
	got := mc.InsertAtAll("hello", "-")
	if got != "h-el-lo" {
		t.Fatalf("InsertAtAll() = %q, want %q", got, "h-el-lo")
	}
	cursors := mc.Cursors()
	if len(cursors) != 2 {
		t.Fatalf("cursor count = %d, want 2", len(cursors))
	}
	if cursors[0].Offset != 2 {
		t.Fatalf("primary offset = %d, want 2", cursors[0].Offset)
	}
	if cursors[1].Offset != 5 {
		t.Fatalf("secondary offset = %d, want 5", cursors[1].Offset)
	}
}

func TestMultiCursorDeleteBackspace(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(1, 3)
	mc.AddSelection(4, 7)
	got := mc.DeleteBackspace("hello world")
	if got != "hlorld" {
		t.Fatalf("DeleteBackspace() = %q, want %q", got, "hlorld")
	}
}

func TestMultiCursorDeleteForward(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(0, 0)
	mc.AddCursor(1)
	got := mc.DeleteForward("abc")
	if got != "c" {
		t.Fatalf("DeleteForward() = %q, want %q", got, "c")
	}
}

func TestMultiCursorAddNextOccurrence(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(0, 3)

	if !mc.AddNextOccurrence("foo foo foo") {
		t.Fatalf("first AddNextOccurrence() = false, want true")
	}
	if !mc.AddNextOccurrence("foo foo foo") {
		t.Fatalf("second AddNextOccurrence() = false, want true")
	}
	if mc.AddNextOccurrence("foo foo foo") {
		t.Fatalf("third AddNextOccurrence() = true, want false")
	}

	if mc.Count() != 3 {
		t.Fatalf("cursor count = %d, want 3", mc.Count())
	}
}

func TestMultiCursorReset(t *testing.T) {
	mc := NewMultiCursor()
	mc.SetPrimary(10, 2)
	mc.AddCursor(5)
	mc.AddCursor(7)
	mc.Reset()
	if mc.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", mc.Count())
	}
	primary := mc.Primary()
	if primary.Offset != 10 || primary.Anchor != 2 {
		t.Fatalf("Primary() = %+v, want {10 2}", primary)
	}
}
