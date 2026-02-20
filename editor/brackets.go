package editor

// bracketPairs maps each bracket character to its matching partner.
var bracketPairs = map[rune]rune{
	'(': ')',
	')': '(',
	'{': '}',
	'}': '{',
	'[': ']',
	']': '[',
}

// openBrackets is the set of opening bracket characters.
var openBrackets = map[rune]bool{
	'(': true,
	'{': true,
	'[': true,
}

// FindMatchingBracket finds the matching bracket for the bracket at the given
// rune position. Returns the rune position of the match and true, or 0 and
// false if no match is found or the position is not a bracket.
// Supports: () {} []
func FindMatchingBracket(text string, pos int) (int, bool) {
	runes := []rune(text)
	if pos < 0 || pos >= len(runes) {
		return 0, false
	}

	ch := runes[pos]
	partner, isBracket := bracketPairs[ch]
	if !isBracket {
		return 0, false
	}

	if openBrackets[ch] {
		// Scan forward for matching close bracket.
		depth := 1
		for i := pos + 1; i < len(runes); i++ {
			if runes[i] == ch {
				depth++
			} else if runes[i] == partner {
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	} else {
		// Scan backward for matching open bracket.
		depth := 1
		for i := pos - 1; i >= 0; i-- {
			if runes[i] == ch {
				depth++
			} else if runes[i] == partner {
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	}

	return 0, false
}
