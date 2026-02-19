package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTabManagerEmpty(t *testing.T) {
	tm := NewTabManager()
	if tm == nil {
		t.Fatal("NewTabManager returned nil")
	}
	if tm.Count() != 0 {
		t.Errorf("Count = %d, want 0", tm.Count())
	}
	if tm.Active() != -1 {
		t.Errorf("Active = %d, want -1", tm.Active())
	}
	if tm.ActiveBuffer() != nil {
		t.Error("ActiveBuffer should be nil when empty")
	}
	if bufs := tm.Buffers(); len(bufs) != 0 {
		t.Errorf("Buffers length = %d, want 0", len(bufs))
	}
}

func TestNewUntitled(t *testing.T) {
	tm := NewTabManager()

	idx := tm.NewUntitled()
	if idx != 0 {
		t.Errorf("first NewUntitled index = %d, want 0", idx)
	}
	if tm.Count() != 1 {
		t.Errorf("Count = %d, want 1", tm.Count())
	}
	if tm.Active() != 0 {
		t.Errorf("Active = %d, want 0", tm.Active())
	}

	buf := tm.ActiveBuffer()
	if buf == nil {
		t.Fatal("ActiveBuffer should not be nil")
	}
	if !buf.Untitled() {
		t.Error("buffer should be untitled")
	}
	if buf.Title() != "untitled" {
		t.Errorf("Title = %q, want %q", buf.Title(), "untitled")
	}
}

func TestNewUntitledMultiple(t *testing.T) {
	tm := NewTabManager()

	idx0 := tm.NewUntitled()
	idx1 := tm.NewUntitled()
	idx2 := tm.NewUntitled()

	if idx0 != 0 || idx1 != 1 || idx2 != 2 {
		t.Errorf("indices = (%d, %d, %d), want (0, 1, 2)", idx0, idx1, idx2)
	}
	if tm.Count() != 3 {
		t.Errorf("Count = %d, want 3", tm.Count())
	}
	// Last NewUntitled should be active.
	if tm.Active() != 2 {
		t.Errorf("Active = %d, want 2", tm.Active())
	}
}

func TestTabOpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tm := NewTabManager()
	idx, err := tm.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if idx != 0 {
		t.Errorf("index = %d, want 0", idx)
	}
	if tm.Count() != 1 {
		t.Errorf("Count = %d, want 1", tm.Count())
	}
	if tm.Active() != 0 {
		t.Errorf("Active = %d, want 0", tm.Active())
	}

	buf := tm.ActiveBuffer()
	if buf == nil {
		t.Fatal("ActiveBuffer should not be nil")
	}
	if buf.Text() != "hello" {
		t.Errorf("Text = %q, want %q", buf.Text(), "hello")
	}
	if buf.Title() != "hello.txt" {
		t.Errorf("Title = %q, want %q", buf.Title(), "hello.txt")
	}
}

func TestOpenFileNonexistent(t *testing.T) {
	tm := NewTabManager()
	_, err := tm.OpenFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("OpenFile with nonexistent path should return error")
	}
	if tm.Count() != 0 {
		t.Errorf("Count = %d after failed open, want 0", tm.Count())
	}
	if tm.Active() != -1 {
		t.Errorf("Active = %d after failed open, want -1", tm.Active())
	}
}

func TestOpenFileDeduplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tm := NewTabManager()

	idx1, err := tm.OpenFile(path)
	if err != nil {
		t.Fatalf("first OpenFile: %v", err)
	}

	// Open another file to change active.
	tm.NewUntitled()
	if tm.Active() != 1 {
		t.Fatalf("Active = %d, want 1 after NewUntitled", tm.Active())
	}

	// Re-open the same path: should not duplicate.
	idx2, err := tm.OpenFile(path)
	if err != nil {
		t.Fatalf("second OpenFile: %v", err)
	}

	if idx2 != idx1 {
		t.Errorf("second open index = %d, want %d (same as first)", idx2, idx1)
	}
	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2 (no duplicate)", tm.Count())
	}
	// Active should switch to the existing buffer.
	if tm.Active() != idx1 {
		t.Errorf("Active = %d, want %d after re-open", tm.Active(), idx1)
	}
}

func TestOpenFileMultiple(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("aaa"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("bbb"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tm := NewTabManager()

	idxA, err := tm.OpenFile(pathA)
	if err != nil {
		t.Fatalf("OpenFile a: %v", err)
	}

	idxB, err := tm.OpenFile(pathB)
	if err != nil {
		t.Fatalf("OpenFile b: %v", err)
	}

	if idxA != 0 || idxB != 1 {
		t.Errorf("indices = (%d, %d), want (0, 1)", idxA, idxB)
	}
	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	if tm.Active() != 1 {
		t.Errorf("Active = %d, want 1 (last opened)", tm.Active())
	}
}

func TestSetActive(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()
	tm.NewUntitled()
	tm.NewUntitled()

	tm.SetActive(0)
	if tm.Active() != 0 {
		t.Errorf("Active = %d, want 0", tm.Active())
	}

	tm.SetActive(2)
	if tm.Active() != 2 {
		t.Errorf("Active = %d, want 2", tm.Active())
	}
}

func TestSetActiveOutOfRange(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()

	// Negative index: should not change active.
	tm.SetActive(-1)
	if tm.Active() != 0 {
		t.Errorf("Active = %d after SetActive(-1), want 0", tm.Active())
	}

	// Too large: should not change active.
	tm.SetActive(5)
	if tm.Active() != 0 {
		t.Errorf("Active = %d after SetActive(5), want 0", tm.Active())
	}
}

func TestSetActiveEmpty(t *testing.T) {
	tm := NewTabManager()
	// Should be no-op on empty manager.
	tm.SetActive(0)
	if tm.Active() != -1 {
		t.Errorf("Active = %d after SetActive(0) on empty, want -1", tm.Active())
	}
}

func TestBufferByIndex(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()
	tm.NewUntitled()

	buf0 := tm.Buffer(0)
	if buf0 == nil {
		t.Fatal("Buffer(0) should not be nil")
	}

	buf1 := tm.Buffer(1)
	if buf1 == nil {
		t.Fatal("Buffer(1) should not be nil")
	}

	if buf0 == buf1 {
		t.Error("Buffer(0) and Buffer(1) should be different instances")
	}
}

func TestBufferOutOfRange(t *testing.T) {
	tm := NewTabManager()
	if tm.Buffer(0) != nil {
		t.Error("Buffer(0) on empty manager should be nil")
	}
	if tm.Buffer(-1) != nil {
		t.Error("Buffer(-1) should be nil")
	}
	if tm.Buffer(100) != nil {
		t.Error("Buffer(100) should be nil")
	}

	tm.NewUntitled()
	if tm.Buffer(1) != nil {
		t.Error("Buffer(1) on single-tab manager should be nil")
	}
}

func TestCloseOnly(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()

	tm.Close(0)
	if tm.Count() != 0 {
		t.Errorf("Count = %d, want 0", tm.Count())
	}
	if tm.Active() != -1 {
		t.Errorf("Active = %d, want -1", tm.Active())
	}
	if tm.ActiveBuffer() != nil {
		t.Error("ActiveBuffer should be nil after closing last buffer")
	}
}

func TestCloseActiveFirst(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled() // 0
	tm.NewUntitled() // 1
	tm.NewUntitled() // 2

	// Active is 2 (the last one created).
	tm.SetActive(0)
	tm.Close(0) // Close active (first). Remaining: [1,2] -> indices [0,1].

	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	// After closing index 0, active should clamp to 0 (the new first).
	if tm.Active() < 0 || tm.Active() >= tm.Count() {
		t.Errorf("Active = %d, out of valid range [0, %d)", tm.Active(), tm.Count())
	}
}

func TestCloseActiveLast(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled() // 0
	tm.NewUntitled() // 1
	tm.NewUntitled() // 2

	// Active is 2 (the last one).
	tm.Close(2) // Close last (active).

	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	// Active should clamp to the new last valid index (1).
	if tm.Active() != 1 {
		t.Errorf("Active = %d, want 1", tm.Active())
	}
}

func TestCloseBeforeActive(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled() // 0
	tm.NewUntitled() // 1
	tm.NewUntitled() // 2

	tm.SetActive(2)
	tm.Close(0) // Close before active. Active was 2, should become 1.

	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	if tm.Active() != 1 {
		t.Errorf("Active = %d, want 1 (shifted after closing before active)", tm.Active())
	}
}

func TestCloseAfterActive(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled() // 0
	tm.NewUntitled() // 1
	tm.NewUntitled() // 2

	tm.SetActive(0)
	tm.Close(2) // Close after active. Active stays at 0.

	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	if tm.Active() != 0 {
		t.Errorf("Active = %d, want 0 (unchanged after closing after active)", tm.Active())
	}
}

func TestCloseMiddle(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled() // 0
	tm.NewUntitled() // 1
	tm.NewUntitled() // 2

	tm.SetActive(1)
	tm.Close(1) // Close active middle tab.

	if tm.Count() != 2 {
		t.Errorf("Count = %d, want 2", tm.Count())
	}
	// Active should clamp to valid range: index 1 is still valid (was index 2).
	if tm.Active() < 0 || tm.Active() >= tm.Count() {
		t.Errorf("Active = %d, out of valid range [0, %d)", tm.Active(), tm.Count())
	}
}

func TestCloseOutOfRange(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()

	// Should be no-ops.
	tm.Close(-1)
	tm.Close(5)
	if tm.Count() != 1 {
		t.Errorf("Count = %d after invalid close, want 1", tm.Count())
	}
}

func TestMultipleOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	pathC := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(pathA, []byte("a"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("b"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathC, []byte("c"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tm := NewTabManager()

	// Open three files.
	tm.OpenFile(pathA) // 0
	tm.OpenFile(pathB) // 1
	tm.OpenFile(pathC) // 2

	if tm.Count() != 3 {
		t.Fatalf("Count = %d, want 3", tm.Count())
	}

	// Close middle.
	tm.Close(1)
	if tm.Count() != 2 {
		t.Fatalf("Count = %d, want 2", tm.Count())
	}

	// Remaining should be a.txt and c.txt.
	if tm.Buffer(0).Title() != "a.txt" {
		t.Errorf("Buffer(0).Title = %q, want %q", tm.Buffer(0).Title(), "a.txt")
	}
	if tm.Buffer(1).Title() != "c.txt" {
		t.Errorf("Buffer(1).Title = %q, want %q", tm.Buffer(1).Title(), "c.txt")
	}

	// Add a new untitled.
	idx := tm.NewUntitled()
	if idx != 2 {
		t.Errorf("NewUntitled index = %d, want 2", idx)
	}
	if tm.Count() != 3 {
		t.Errorf("Count = %d, want 3", tm.Count())
	}

	// Close all.
	tm.Close(0)
	tm.Close(0)
	tm.Close(0)
	if tm.Count() != 0 {
		t.Errorf("Count = %d, want 0", tm.Count())
	}
	if tm.Active() != -1 {
		t.Errorf("Active = %d, want -1", tm.Active())
	}
}

func TestBuffersReturnsSlice(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()
	tm.NewUntitled()

	bufs := tm.Buffers()
	if len(bufs) != 2 {
		t.Errorf("Buffers length = %d, want 2", len(bufs))
	}

	// Verify they match Buffer(i).
	for i, buf := range bufs {
		if buf != tm.Buffer(i) {
			t.Errorf("Buffers()[%d] != Buffer(%d)", i, i)
		}
	}
}

func TestCloseAllSequentially(t *testing.T) {
	tm := NewTabManager()
	tm.NewUntitled()
	tm.NewUntitled()
	tm.NewUntitled()

	// Close from the end.
	tm.Close(2)
	if tm.Count() != 2 || tm.Active() != 1 {
		t.Errorf("after close(2): Count=%d Active=%d, want Count=2 Active=1", tm.Count(), tm.Active())
	}

	tm.Close(1)
	if tm.Count() != 1 || tm.Active() != 0 {
		t.Errorf("after close(1): Count=%d Active=%d, want Count=1 Active=0", tm.Count(), tm.Active())
	}

	tm.Close(0)
	if tm.Count() != 0 || tm.Active() != -1 {
		t.Errorf("after close(0): Count=%d Active=%d, want Count=0 Active=-1", tm.Count(), tm.Active())
	}
}

func TestOpenFileSetsActive(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("a"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("b"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tm := NewTabManager()
	tm.OpenFile(pathA)
	if tm.Active() != 0 {
		t.Errorf("Active = %d after first open, want 0", tm.Active())
	}

	tm.OpenFile(pathB)
	if tm.Active() != 1 {
		t.Errorf("Active = %d after second open, want 1", tm.Active())
	}

	// Go back to first.
	tm.SetActive(0)
	if tm.Active() != 0 {
		t.Errorf("Active = %d after SetActive(0), want 0", tm.Active())
	}

	// ActiveBuffer should be the first file.
	buf := tm.ActiveBuffer()
	if buf.Title() != "a.txt" {
		t.Errorf("ActiveBuffer title = %q, want %q", buf.Title(), "a.txt")
	}
}
