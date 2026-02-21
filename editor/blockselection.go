package editor

import "strings"

// BlockSelection represents a rectangular text selection spanning multiple lines.
type BlockSelection struct {
	StartLine int
	EndLine   int
	StartCol  int
	EndCol    int
	Active    bool
}

// NewBlockSelection creates an inactive block selection.
func NewBlockSelection() *BlockSelection {
	return &BlockSelection{}
}

// Set activates the block selection with the given bounds.
func (bs *BlockSelection) Set(startLine, endLine, startCol, endCol int) {
	bs.StartLine = startLine
	bs.EndLine = endLine
	bs.StartCol = startCol
	bs.EndCol = endCol
	bs.Active = true
	bs.Normalize()
}

// Clear deactivates the block selection.
func (bs *BlockSelection) Clear() {
	bs.Active = false
	bs.StartLine = 0
	bs.EndLine = 0
	bs.StartCol = 0
	bs.EndCol = 0
}

// Normalize ensures StartLine <= EndLine and StartCol <= EndCol.
func (bs *BlockSelection) Normalize() {
	if bs.StartLine > bs.EndLine {
		bs.StartLine, bs.EndLine = bs.EndLine, bs.StartLine
	}
	if bs.StartCol > bs.EndCol {
		bs.StartCol, bs.EndCol = bs.EndCol, bs.StartCol
	}
}

// ExpandUp extends the selection one line upward.
func (bs *BlockSelection) ExpandUp() {
	if bs.StartLine > 0 {
		bs.StartLine--
	}
}

// ExpandDown extends the selection one line downward.
func (bs *BlockSelection) ExpandDown(maxLine int) {
	if bs.EndLine < maxLine {
		bs.EndLine++
	}
}

// ExpandLeft extends the selection one column to the left.
func (bs *BlockSelection) ExpandLeft() {
	if bs.StartCol > 0 {
		bs.StartCol--
	}
}

// ExpandRight extends the selection one column to the right.
func (bs *BlockSelection) ExpandRight(maxCol int) {
	if bs.EndCol < maxCol {
		bs.EndCol++
	}
}

// Lines returns the range of lines in the selection [start, end] inclusive.
func (bs *BlockSelection) Lines() (int, int) {
	return bs.StartLine, bs.EndLine
}

// Cols returns the range of columns [start, end) for the selection.
func (bs *BlockSelection) Cols() (int, int) {
	return bs.StartCol, bs.EndCol
}

// ExtractBlock extracts the selected rectangular region from text as a
// slice of strings (one per line).
func (bs *BlockSelection) ExtractBlock(text string) []string {
	if !bs.Active {
		return nil
	}
	lines := strings.Split(text, "\n")
	var result []string

	for i := bs.StartLine; i <= bs.EndLine && i < len(lines); i++ {
		runes := []rune(lines[i])
		start := bs.StartCol
		end := bs.EndCol
		if start > len(runes) {
			start = len(runes)
		}
		if end > len(runes) {
			end = len(runes)
		}
		if start > end {
			start = end
		}
		result = append(result, string(runes[start:end]))
	}
	return result
}

// InsertAtBlock inserts text at each line of the block selection at the start column.
func (bs *BlockSelection) InsertAtBlock(text string, insert string) string {
	if !bs.Active {
		return text
	}
	lines := strings.Split(text, "\n")
	insertRunes := []rune(insert)

	for i := bs.StartLine; i <= bs.EndLine && i < len(lines); i++ {
		runes := []rune(lines[i])
		col := bs.StartCol
		if col > len(runes) {
			// Pad with spaces
			pad := make([]rune, col-len(runes))
			for j := range pad {
				pad[j] = ' '
			}
			runes = append(runes, pad...)
		}
		newRunes := make([]rune, 0, len(runes)+len(insertRunes))
		newRunes = append(newRunes, runes[:col]...)
		newRunes = append(newRunes, insertRunes...)
		newRunes = append(newRunes, runes[col:]...)
		lines[i] = string(newRunes)
	}
	return strings.Join(lines, "\n")
}

// DeleteBlock removes the selected rectangular region from the text.
func (bs *BlockSelection) DeleteBlock(text string) string {
	if !bs.Active {
		return text
	}
	lines := strings.Split(text, "\n")

	for i := bs.StartLine; i <= bs.EndLine && i < len(lines); i++ {
		runes := []rune(lines[i])
		start := bs.StartCol
		end := bs.EndCol
		if start > len(runes) {
			continue // nothing to delete on this line
		}
		if end > len(runes) {
			end = len(runes)
		}
		newRunes := make([]rune, 0, len(runes)-(end-start))
		newRunes = append(newRunes, runes[:start]...)
		newRunes = append(newRunes, runes[end:]...)
		lines[i] = string(newRunes)
	}
	return strings.Join(lines, "\n")
}
