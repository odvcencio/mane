package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Range represents a byte range [Start, End) within buffer text.
type Range struct {
	Start, End int
}

// editOp records a single edit for undo/redo support.
type editOp struct {
	offset  int
	oldText string
	newText string
}

// Buffer manages the text content of a single open file.
type Buffer struct {
	path      string // absolute path, or "" if untitled
	text      string // current text content
	savedText string // text at last save/open (for dirty comparison)
	undoStack []editOp
	redoStack []editOp
}

// NewBuffer creates a new empty, untitled buffer.
func NewBuffer() *Buffer {
	return &Buffer{}
}

// Open reads the file at path into the buffer, replacing any existing content.
// The stored path is converted to an absolute path.
func (b *Buffer) Open(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	b.path = absPath
	b.text = string(data)
	b.savedText = b.text
	return nil
}

// Save writes the current text to the stored path.
// Returns an error if the buffer has no path (untitled).
func (b *Buffer) Save() error {
	if b.path == "" {
		return errors.New("buffer has no path; use SaveAs")
	}
	if err := os.WriteFile(b.path, []byte(b.text), 0644); err != nil {
		return err
	}
	b.savedText = b.text
	return nil
}

// SaveAs writes the current text to the given path, updates the stored path,
// and marks the buffer as clean.
func (b *Buffer) SaveAs(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := os.WriteFile(absPath, []byte(b.text), 0644); err != nil {
		return err
	}

	b.path = absPath
	b.savedText = b.text
	return nil
}

// Path returns the absolute file path, or "" if the buffer is untitled.
func (b *Buffer) Path() string {
	return b.path
}

// Text returns the current text content of the buffer.
func (b *Buffer) Text() string {
	return b.text
}

// SetText updates the buffer's text content. Dirty status is computed by
// comparing the current text with the saved text.
func (b *Buffer) SetText(text string) {
	b.text = text
}

// Dirty reports whether the buffer's text differs from the last saved/opened text.
func (b *Buffer) Dirty() bool {
	return b.text != b.savedText
}

// Untitled reports whether the buffer has no associated file path.
func (b *Buffer) Untitled() bool {
	return b.path == ""
}

// Title returns the base filename, or "untitled" if the buffer has no path.
func (b *Buffer) Title() string {
	if b.path == "" {
		return "untitled"
	}
	return filepath.Base(b.path)
}

// ApplyEdit records the edit on the undo stack, clears the redo stack,
// and applies the edit to the buffer text. The edit replaces the text at
// [offset, offset+len(oldText)) with newText.
func (b *Buffer) ApplyEdit(offset int, oldText, newText string) {
	b.undoStack = append(b.undoStack, editOp{
		offset:  offset,
		oldText: oldText,
		newText: newText,
	})
	b.redoStack = nil
	b.text = b.text[:offset] + newText + b.text[offset+len(oldText):]
}

// Undo reverses the last edit. Returns true if an edit was undone, false if
// the undo stack is empty.
func (b *Buffer) Undo() bool {
	if len(b.undoStack) == 0 {
		return false
	}
	op := b.undoStack[len(b.undoStack)-1]
	b.undoStack = b.undoStack[:len(b.undoStack)-1]
	// Reverse the edit: replace newText back with oldText.
	b.text = b.text[:op.offset] + op.oldText + b.text[op.offset+len(op.newText):]
	b.redoStack = append(b.redoStack, op)
	return true
}

// Redo reapplies the last undone edit. Returns true if an edit was redone,
// false if the redo stack is empty.
func (b *Buffer) Redo() bool {
	if len(b.redoStack) == 0 {
		return false
	}
	op := b.redoStack[len(b.redoStack)-1]
	b.redoStack = b.redoStack[:len(b.redoStack)-1]
	// Reapply the edit.
	b.text = b.text[:op.offset] + op.newText + b.text[op.offset+len(op.oldText):]
	b.undoStack = append(b.undoStack, op)
	return true
}

// Find returns all byte ranges where query appears as a substring in the
// buffer text. Returns nil if query is empty or not found.
func (b *Buffer) Find(query string) []Range {
	if query == "" {
		return nil
	}
	var results []Range
	start := 0
	for {
		idx := strings.Index(b.text[start:], query)
		if idx < 0 {
			break
		}
		absIdx := start + idx
		results = append(results, Range{Start: absIdx, End: absIdx + len(query)})
		start = absIdx + len(query)
	}
	return results
}

// Replace replaces the text at the given range with replacement, recording
// the edit on the undo stack.
func (b *Buffer) Replace(query, replacement string, r Range) {
	oldText := b.text[r.Start:r.End]
	b.ApplyEdit(r.Start, oldText, replacement)
}

// ReplaceAll replaces all occurrences of query with replacement. Returns the
// number of replacements made. Each replacement is recorded as a single undo
// operation processed from back to front so that offsets remain valid.
func (b *Buffer) ReplaceAll(query, replacement string) int {
	ranges := b.Find(query)
	if len(ranges) == 0 {
		return 0
	}
	// Apply replacements from back to front so earlier offsets stay valid.
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		b.ApplyEdit(r.Start, query, replacement)
	}
	return len(ranges)
}
