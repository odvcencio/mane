package editor

import "strings"

// DetectIndentStyle looks at the text to determine whether tabs or spaces are
// used for indentation. Returns the indent unit string (e.g., "\t" or "    ").
// Defaults to "\t" if no indentation found.
func DetectIndentStyle(text string) string {
	tabCount := 0
	spaceCount := 0
	minSpaceWidth := 0

	for line := range strings.SplitSeq(text, "\n") {
		if len(line) == 0 {
			continue
		}
		if line[0] == '\t' {
			tabCount++
		} else if line[0] == ' ' {
			spaceCount++
			// Count the leading spaces on this line.
			w := 0
			for _, ch := range line {
				if ch == ' ' {
					w++
				} else {
					break
				}
			}
			if w > 0 && (minSpaceWidth == 0 || w < minSpaceWidth) {
				minSpaceWidth = w
			}
		}
	}

	if spaceCount > tabCount && minSpaceWidth > 0 {
		return strings.Repeat(" ", minSpaceWidth)
	}
	return "\t"
}

// ComputeIndent returns the indentation string to use for a new line after
// the given line. It copies the existing indent and increases it if the line
// ends with a block-opening token ({, (, [, :) after trimming trailing
// whitespace.
func ComputeIndent(line string) string {
	// Extract leading whitespace.
	indent := ""
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			indent += string(ch)
		} else {
			break
		}
	}

	trimmed := strings.TrimRight(line, " \t")
	if len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		switch last {
		case '{', '(', '[':
			// Determine indent unit from current indent.
			if strings.Contains(indent, "\t") || indent == "" {
				indent += "\t"
			} else {
				indent += "    "
			}
		case ':':
			// Python-style blocks: increase indentation after colon.
			if strings.Contains(indent, "\t") || indent == "" {
				indent += "\t"
			} else {
				// Reuse the current space indentation width.
				spaces := 0
				for _, ch := range indent {
					if ch != ' ' {
						break
					}
					spaces++
				}
				if spaces <= 0 {
					spaces = 4
				}
				indent += strings.Repeat(" ", spaces)
			}
		}
	}

	return indent
}
