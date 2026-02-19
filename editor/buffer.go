package editor

import (
	"errors"
	"os"
	"path/filepath"
)

// Buffer manages the text content of a single open file.
type Buffer struct {
	path      string // absolute path, or "" if untitled
	text      string // current text content
	savedText string // text at last save/open (for dirty comparison)
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
