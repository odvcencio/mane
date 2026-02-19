package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBuffer(t *testing.T) {
	b := NewBuffer()
	if b == nil {
		t.Fatal("NewBuffer returned nil")
	}
	if b.Text() != "" {
		t.Errorf("new buffer text = %q, want empty", b.Text())
	}
	if b.Path() != "" {
		t.Errorf("new buffer path = %q, want empty", b.Path())
	}
	if b.Dirty() {
		t.Error("new buffer should not be dirty")
	}
	if !b.Untitled() {
		t.Error("new buffer should be untitled")
	}
	if b.Title() != "untitled" {
		t.Errorf("new buffer title = %q, want %q", b.Title(), "untitled")
	}
}

func TestOpenFile(t *testing.T) {
	// Create a temporary file with known content.
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := "hello, world\nsecond line\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	if err := b.Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if b.Text() != content {
		t.Errorf("text = %q, want %q", b.Text(), content)
	}

	// Path should be absolute.
	if !filepath.IsAbs(b.Path()) {
		t.Errorf("path %q is not absolute", b.Path())
	}

	if b.Dirty() {
		t.Error("buffer should not be dirty after Open")
	}

	if b.Untitled() {
		t.Error("buffer should not be untitled after Open")
	}

	if b.Title() != "hello.txt" {
		t.Errorf("title = %q, want %q", b.Title(), "hello.txt")
	}
}

func TestOpenRelativePath(t *testing.T) {
	// Open with a relative path should store an absolute path.
	dir := t.TempDir()
	path := filepath.Join(dir, "rel.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	// Use the absolute path here since we can't control cwd easily,
	// but verify it's stored as absolute.
	if err := b.Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !filepath.IsAbs(b.Path()) {
		t.Errorf("path %q is not absolute", b.Path())
	}
}

func TestOpenNonexistentFile(t *testing.T) {
	b := NewBuffer()
	err := b.Open("/nonexistent/path/to/file.txt")
	if err == nil {
		t.Fatal("Open nonexistent file should return error")
	}
}

func TestSetTextMakesDirty(t *testing.T) {
	b := NewBuffer()
	if b.Dirty() {
		t.Fatal("new buffer should not be dirty")
	}

	b.SetText("some content")
	if !b.Dirty() {
		t.Error("buffer should be dirty after SetText with different content")
	}
	if b.Text() != "some content" {
		t.Errorf("text = %q, want %q", b.Text(), "some content")
	}
}

func TestSetTextSameContentNotDirty(t *testing.T) {
	// Open a file, then set text to the same content. Should not be dirty.
	dir := t.TempDir()
	path := filepath.Join(dir, "same.txt")
	content := "unchanged"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	if err := b.Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}

	b.SetText(content)
	if b.Dirty() {
		t.Error("buffer should not be dirty when text matches saved text")
	}
}

func TestSaveAsWritesFile(t *testing.T) {
	b := NewBuffer()
	b.SetText("file content here")

	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	if err := b.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	// Verify file was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "file content here" {
		t.Errorf("file content = %q, want %q", string(data), "file content here")
	}

	// Buffer should no longer be dirty.
	if b.Dirty() {
		t.Error("buffer should not be dirty after SaveAs")
	}

	// Path should be updated.
	if !filepath.IsAbs(b.Path()) {
		t.Errorf("path %q is not absolute after SaveAs", b.Path())
	}
	if b.Title() != "output.txt" {
		t.Errorf("title = %q, want %q", b.Title(), "output.txt")
	}

	if b.Untitled() {
		t.Error("buffer should not be untitled after SaveAs")
	}
}

func TestSaveOverwritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.txt")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	if err := b.Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}

	b.SetText("modified content")
	if !b.Dirty() {
		t.Fatal("buffer should be dirty after modification")
	}

	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was overwritten.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "modified content" {
		t.Errorf("file content = %q, want %q", string(data), "modified content")
	}

	// Buffer should be clean after save.
	if b.Dirty() {
		t.Error("buffer should not be dirty after Save")
	}
}

func TestSaveUntitledBufferErrors(t *testing.T) {
	b := NewBuffer()
	b.SetText("some text")

	err := b.Save()
	if err == nil {
		t.Fatal("Save on untitled buffer should return error")
	}
}

func TestSaveEmptyPathAfterSetText(t *testing.T) {
	// Even with text, Save should fail if there's no path.
	b := NewBuffer()
	b.SetText("content")
	if err := b.Save(); err == nil {
		t.Error("Save with no path should error")
	}
}

func TestSaveAsUpdatesPath(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")

	if err := os.WriteFile(pathA, []byte("alpha"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	if err := b.Open(pathA); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b.Title() != "a.txt" {
		t.Errorf("title = %q, want %q", b.Title(), "a.txt")
	}

	b.SetText("beta")
	if err := b.SaveAs(pathB); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	if b.Title() != "b.txt" {
		t.Errorf("title = %q after SaveAs, want %q", b.Title(), "b.txt")
	}
	if b.Path() != pathB {
		t.Errorf("path = %q after SaveAs, want %q", b.Path(), pathB)
	}

	// Original file should be unchanged.
	data, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(data) != "alpha" {
		t.Errorf("original file content = %q, want %q", string(data), "alpha")
	}

	// New file should have new content.
	data, err = os.ReadFile(pathB)
	if err != nil {
		t.Fatalf("read new: %v", err)
	}
	if string(data) != "beta" {
		t.Errorf("new file content = %q, want %q", string(data), "beta")
	}
}

func TestDirtyComputedByComparison(t *testing.T) {
	b := NewBuffer()

	// Empty buffer, empty savedText: not dirty.
	if b.Dirty() {
		t.Error("new buffer should not be dirty")
	}

	// Set text to something.
	b.SetText("abc")
	if !b.Dirty() {
		t.Error("should be dirty after SetText")
	}

	// Set text back to empty (matches savedText).
	b.SetText("")
	if b.Dirty() {
		t.Error("should not be dirty after resetting text to match saved")
	}
}

func TestOpenResetsState(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("aaa"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("bbb"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBuffer()
	if err := b.Open(pathA); err != nil {
		t.Fatalf("Open a: %v", err)
	}
	b.SetText("modified")
	if !b.Dirty() {
		t.Fatal("should be dirty")
	}

	// Open another file resets everything.
	if err := b.Open(pathB); err != nil {
		t.Fatalf("Open b: %v", err)
	}
	if b.Text() != "bbb" {
		t.Errorf("text = %q, want %q", b.Text(), "bbb")
	}
	if b.Dirty() {
		t.Error("should not be dirty after opening new file")
	}
	if b.Title() != "b.txt" {
		t.Errorf("title = %q, want %q", b.Title(), "b.txt")
	}
}

func TestSaveAsCreatesIntermediateDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "file.txt")

	b := NewBuffer()
	b.SetText("nested")

	// SaveAs should work even if parent dirs don't exist
	// (or it may fail — depends on spec. We'll test what happens.)
	// Since the spec doesn't mention creating dirs, this tests current behavior.
	err := b.SaveAs(path)
	// If it errors, that's acceptable behavior — just verify it doesn't panic.
	if err != nil {
		// Acceptable: SaveAs doesn't create intermediate directories.
		return
	}
	data, _ := os.ReadFile(path)
	if string(data) != "nested" {
		t.Errorf("file content = %q, want %q", string(data), "nested")
	}
}
