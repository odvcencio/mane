package editor

import (
	"sort"
	"strings"
)

// Cursor represents one cursor with an optional selection.
// Offsets are rune offsets into the document.
type Cursor struct {
	Offset int // Cursor position.
	Anchor int // Selection anchor; same as Offset for no selection.
}

// MultiCursor stores a set of independent cursors.
type MultiCursor struct {
	cursors []Cursor
}

// NewMultiCursor returns a single cursor at offset 0.
func NewMultiCursor() *MultiCursor {
	return &MultiCursor{cursors: []Cursor{{Offset: 0, Anchor: 0}}}
}

// Cursors returns the currently tracked cursors.
func (mc *MultiCursor) Cursors() []Cursor {
	if mc == nil {
		return nil
	}
	out := make([]Cursor, len(mc.cursors))
	copy(out, mc.cursors)
	return out
}

// Primary returns the first cursor (the primary editing position).
func (mc *MultiCursor) Primary() Cursor {
	if mc == nil || len(mc.cursors) == 0 {
		return Cursor{}
	}
	return mc.cursors[0]
}

// Count reports how many cursors are active.
func (mc *MultiCursor) Count() int {
	if mc == nil {
		return 0
	}
	return len(mc.cursors)
}

// IsMulti reports whether more than one cursor exists.
func (mc *MultiCursor) IsMulti() bool {
	return mc.Count() > 1
}

// Reset keeps only the primary cursor.
func (mc *MultiCursor) Reset() {
	if mc == nil {
		return
	}
	if len(mc.cursors) == 0 {
		mc.cursors = []Cursor{{Offset: 0, Anchor: 0}}
		return
	}
	mc.cursors = mc.cursors[:1]
	c0 := mc.cursors[0]
	mc.cursors[0] = Cursor{Offset: maxInt(0, c0.Offset), Anchor: maxInt(0, c0.Anchor)}
}

// SetPrimary updates the primary cursor and keeps it as the first cursor.
func (mc *MultiCursor) SetPrimary(offset, anchor int) {
	if mc == nil {
		return
	}
	if len(mc.cursors) == 0 {
		mc.cursors = []Cursor{{Offset: offset, Anchor: anchor}}
		return
	}
	mc.cursors[0] = Cursor{Offset: offset, Anchor: anchor}
}

// AddCursor appends a cursor at the given offset.
func (mc *MultiCursor) AddCursor(offset int) {
	if mc == nil {
		return
	}
	mc.cursors = append(mc.cursors, Cursor{Offset: offset, Anchor: offset})
}

// AddSelection appends a cursor+selection.
func (mc *MultiCursor) AddSelection(start, end int) {
	if mc == nil {
		return
	}
	mc.cursors = append(mc.cursors, Cursor{Offset: end, Anchor: start})
}

// AddNextOccurrence selects the next occurrence of the last cursor's selection.
// If no such occurrence exists, this returns false.
func (mc *MultiCursor) AddNextOccurrence(text string) bool {
	if mc == nil || len(mc.cursors) == 0 {
		return false
	}

	last := mc.cursors[len(mc.cursors)-1]
	start, end := orderedRuneRange(last.Offset, last.Anchor)
	if start == end {
		return false
	}

	runes := []rune(text)
	query := string(runes[start:end])
	if query == "" {
		return false
	}
	queryLen := len([]rune(query))

	search := func(from int) int {
		for from < len(runes) {
			idx := strings.Index(string(runes[from:]), query)
			if idx < 0 {
				return -1
			}
			candidate := from + idx
			if candidate < 0 || candidate+queryLen > len(runes) {
				return -1
			}
			if !mc.hasRange(candidate, candidate+queryLen) {
				return candidate
			}
			from = candidate + queryLen
		}
		return -1
	}

	candidate := search(end)
	if candidate < 0 {
		candidate = search(0)
	}
	if candidate < 0 {
		return false
	}

	mc.AddSelection(candidate, candidate+queryLen)
	return true
}

// InsertAtAll inserts the provided text at every cursor (or replaces each
// cursor selection).
func (mc *MultiCursor) InsertAtAll(text string, insert string) string {
	if mc == nil || len(mc.cursors) == 0 {
		return text
	}

	inserted := []rune(insert)
	runeLen := len([]rune(text))
	edits := make([]editRange, 0, len(mc.cursors))
	for _, c := range mc.Cursors() {
		start, end := mc.selectionRange(c, runeLen)
		edits = append(edits, editRange{Start: start, End: end, Text: inserted})
	}
	return mc.applyEdits(text, edits)
}

// DeleteBackspace applies backspace behavior for all cursors.
func (mc *MultiCursor) DeleteBackspace(text string) string {
	runeLen := len([]rune(text))
	edits := make([]editRange, 0, mc.Count())
	for _, c := range mc.Cursors() {
		start, end := mc.selectionRange(c, runeLen)
		if start == end {
			if start == 0 {
				continue
			}
			start--
		}
		edits = append(edits, editRange{Start: start, End: end})
	}
	return mc.applyEdits(text, edits)
}

// DeleteForward applies delete-key behavior for all cursors.
func (mc *MultiCursor) DeleteForward(text string) string {
	runeLen := len([]rune(text))
	edits := make([]editRange, 0, mc.Count())
	for _, c := range mc.Cursors() {
		start, end := mc.selectionRange(c, runeLen)
		if start == end {
			if start >= runeLen {
				continue
			}
			end++
		}
		edits = append(edits, editRange{Start: start, End: end})
	}
	return mc.applyEdits(text, edits)
}

type editRange struct {
	Start int
	End   int
	Text  []rune
}

func (mc *MultiCursor) applyEdits(text string, edits []editRange) string {
	if mc == nil {
		return text
	}
	runes := []rune(text)
	if len(edits) == 0 {
		return text
	}

	for i := range edits {
		if edits[i].Start < 0 {
			edits[i].Start = 0
		}
		if edits[i].End < 0 {
			edits[i].End = 0
		}
		if edits[i].Start > len(runes) {
			edits[i].Start = len(runes)
		}
		if edits[i].End > len(runes) {
			edits[i].End = len(runes)
		}
		if edits[i].Start > edits[i].End {
			edits[i].Start, edits[i].End = edits[i].End, edits[i].Start
		}
	}

	// Apply all edits left-to-right to normalize ordering, then remove
	// duplicate/overlapping entries.
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].Start == edits[j].Start {
			return edits[i].End < edits[j].End
		}
		return edits[i].Start < edits[j].Start
	})
	merged := make([]editRange, 0, len(edits))
	for _, e := range edits {
		if len(merged) == 0 {
			merged = append(merged, e)
			continue
		}

		last := &merged[len(merged)-1]
		if e.Start == last.Start && e.End == last.End {
			continue
		}
		// If overlapping edits are provided, keep the first edit only.
		if e.Start < last.End {
			continue
		}
		merged = append(merged, e)
	}
	edits = merged

	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		runes = append(runes[:e.Start], append(e.Text, runes[e.End:]...)...)
	}

	// Update cursor positions after edits.
	for i := range mc.cursors {
		c := mc.cursors[i]
		c.Offset = updateOffset(c.Offset, edits)
		c.Anchor = updateOffset(c.Anchor, edits)
		if c.Offset < 0 {
			c.Offset = 0
		}
		if c.Anchor < 0 {
			c.Anchor = 0
		}
		if c.Offset > len(runes) {
			c.Offset = len(runes)
		}
		if c.Anchor > len(runes) {
			c.Anchor = len(runes)
		}
		mc.cursors[i] = c
	}

	return string(runes)
}

func updateOffset(offset int, edits []editRange) int {
	cur := offset
	delta := 0
	for _, e := range edits {
		start := e.Start + delta
		end := e.End + delta
		insertLen := len(e.Text)
		oldLen := e.End - e.Start
		deltaLen := insertLen - oldLen

		if cur < start {
			// Cursor is before this edit; no position shift yet.
		} else if cur <= end {
			cur = start + insertLen
		} else {
			cur += deltaLen
		}
		delta += deltaLen
	}
	return cur
}

func (mc *MultiCursor) hasRange(start, end int) bool {
	for _, c := range mc.cursors {
		s, e := orderedRuneRange(c.Offset, c.Anchor)
		if s == start && e == end {
			return true
		}
	}
	return false
}

func (mc *MultiCursor) selectionRange(c Cursor, textLen int) (int, int) {
	start, end := orderedRuneRange(c.Offset, c.Anchor)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > textLen {
		start = textLen
	}
	if end > textLen {
		end = textLen
	}
	return start, end
}

func orderedRuneRange(a, b int) (int, int) {
	if a <= b {
		return a, b
	}
	return b, a
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
