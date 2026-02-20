package editor

import "strings"

// LineCount returns the number of lines in the text.
// An empty string is considered to have 1 line.
func LineCount(text string) int {
	if text == "" {
		return 1
	}
	return strings.Count(text, "\n") + 1
}

// DeleteLine removes the line at the given 0-based line number.
// If the line number is out of range, the text is returned unchanged.
func DeleteLine(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return text
	}

	// Single line: return empty string.
	if len(lines) == 1 {
		return ""
	}

	result := make([]string, 0, len(lines)-1)
	for i, l := range lines {
		if i != line {
			result = append(result, l)
		}
	}
	return strings.Join(result, "\n")
}

// MoveLine moves the line at the given 0-based line number by delta
// (+1 = down, -1 = up). Returns the text unchanged if the target
// position is out of bounds.
func MoveLine(text string, line, delta int) string {
	lines := strings.Split(text, "\n")
	target := line + delta
	if line < 0 || line >= len(lines) || target < 0 || target >= len(lines) {
		return text
	}

	// Swap the two lines.
	lines[line], lines[target] = lines[target], lines[line]
	return strings.Join(lines, "\n")
}

// DuplicateLine duplicates the line at the given 0-based line number,
// inserting the copy immediately after it.
func DuplicateLine(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return text
	}

	result := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		result = append(result, l)
		if i == line {
			result = append(result, l)
		}
	}
	return strings.Join(result, "\n")
}
