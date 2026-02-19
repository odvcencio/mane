package editor

import "path/filepath"

// TabManager tracks open file buffers and which one is active.
// It is pure data management — no UI widget dependency.
type TabManager struct {
	buffers []*Buffer
	active  int // index of active tab, or -1 if none
}

// NewTabManager creates a TabManager with no open buffers.
func NewTabManager() *TabManager {
	return &TabManager{
		active: -1,
	}
}

// Count returns the number of open buffers.
func (tm *TabManager) Count() int {
	return len(tm.buffers)
}

// Active returns the index of the active tab, or -1 if there are no open
// buffers.
func (tm *TabManager) Active() int {
	return tm.active
}

// ActiveBuffer returns the currently active buffer, or nil if there are no
// open buffers.
func (tm *TabManager) ActiveBuffer() *Buffer {
	if tm.active < 0 || tm.active >= len(tm.buffers) {
		return nil
	}
	return tm.buffers[tm.active]
}

// Buffer returns the buffer at the given index, or nil if the index is out
// of range.
func (tm *TabManager) Buffer(index int) *Buffer {
	if index < 0 || index >= len(tm.buffers) {
		return nil
	}
	return tm.buffers[index]
}

// Buffers returns all open buffers in tab order.
func (tm *TabManager) Buffers() []*Buffer {
	return tm.buffers
}

// NewUntitled creates a new empty, untitled buffer, appends it, sets it as
// the active tab, and returns its index.
func (tm *TabManager) NewUntitled() int {
	buf := NewBuffer()
	tm.buffers = append(tm.buffers, buf)
	tm.active = len(tm.buffers) - 1
	return tm.active
}

// OpenFile opens the file at path. If a buffer with the same absolute path
// is already open, it switches to that buffer instead of opening a duplicate.
// The new (or existing) buffer is set as active. Returns the tab index and
// any error from opening the file.
func (tm *TabManager) OpenFile(path string) (int, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}

	// Check for an existing buffer with the same path.
	for i, buf := range tm.buffers {
		if buf.Path() == absPath {
			tm.active = i
			return i, nil
		}
	}

	// Open into a new buffer.
	buf := NewBuffer()
	if err := buf.Open(absPath); err != nil {
		return -1, err
	}

	tm.buffers = append(tm.buffers, buf)
	tm.active = len(tm.buffers) - 1
	return tm.active, nil
}

// SetActive switches the active tab to the given index. If the index is out
// of range, this is a no-op.
func (tm *TabManager) SetActive(index int) {
	if index < 0 || index >= len(tm.buffers) {
		return
	}
	tm.active = index
}

// Close removes the buffer at the given index. If the index is out of range,
// this is a no-op. After removal the active index is adjusted:
//   - If the closed tab was before the active tab, active shifts down by one.
//   - If the closed tab was the active tab (or after it and active is now out
//     of range), active is clamped to the last valid index.
//   - If no buffers remain, active becomes -1.
func (tm *TabManager) Close(index int) {
	if index < 0 || index >= len(tm.buffers) {
		return
	}

	// Remove the buffer at index.
	tm.buffers = append(tm.buffers[:index], tm.buffers[index+1:]...)

	if len(tm.buffers) == 0 {
		tm.active = -1
		return
	}

	if index < tm.active {
		// Closed a tab before the active one: shift active down.
		tm.active--
	} else if index == tm.active {
		// Closed the active tab: clamp to valid range.
		if tm.active >= len(tm.buffers) {
			tm.active = len(tm.buffers) - 1
		}
	}
	// If index > tm.active, active stays the same (still valid).
}
