package editor

import "strings"

// FoldRegion represents a foldable region of text.
type FoldRegion struct {
	StartLine int
	EndLine   int
	Folded    bool
}

// FoldState tracks which regions are folded.
type FoldState struct {
	regions []FoldRegion
}

// NewFoldState creates an empty fold state.
func NewFoldState() *FoldState {
	return &FoldState{}
}

// SetRegions replaces the fold regions (e.g. from tree-sitter parse).
// Preserves fold state for regions that match by start line.
func (fs *FoldState) SetRegions(regions []FoldRegion) {
	oldFolded := make(map[int]bool)
	for _, r := range fs.regions {
		if r.Folded {
			oldFolded[r.StartLine] = true
		}
	}
	for i := range regions {
		if oldFolded[regions[i].StartLine] {
			regions[i].Folded = true
		}
	}
	fs.regions = regions
}

// Toggle folds/unfolds the region at the given line.
func (fs *FoldState) Toggle(line int) bool {
	for i, r := range fs.regions {
		if r.StartLine == line {
			fs.regions[i].Folded = !fs.regions[i].Folded
			return true
		}
	}
	return false
}

// FoldAll folds all regions.
func (fs *FoldState) FoldAll() {
	for i := range fs.regions {
		fs.regions[i].Folded = true
	}
}

// UnfoldAll unfolds all regions.
func (fs *FoldState) UnfoldAll() {
	for i := range fs.regions {
		fs.regions[i].Folded = false
	}
}

// IsLineHidden returns true if the given line is inside a folded region
// (not the start line, which remains visible).
func (fs *FoldState) IsLineHidden(line int) bool {
	for _, r := range fs.regions {
		if r.Folded && line > r.StartLine && line <= r.EndLine {
			return true
		}
	}
	return false
}

// Regions returns all fold regions.
func (fs *FoldState) Regions() []FoldRegion {
	return fs.regions
}

// FoldAtLine finds and folds the innermost region starting at or containing
// the given line.
func (fs *FoldState) FoldAtLine(line int) bool {
	best := -1
	for i, r := range fs.regions {
		if r.Folded {
			continue
		}
		if r.StartLine == line {
			fs.regions[i].Folded = true
			return true
		}
		if line >= r.StartLine && line <= r.EndLine {
			if best < 0 || (r.EndLine-r.StartLine) < (fs.regions[best].EndLine-fs.regions[best].StartLine) {
				best = i
			}
		}
	}
	if best >= 0 {
		fs.regions[best].Folded = true
		return true
	}
	return false
}

// UnfoldAtLine unfolds the region at or containing the given line.
func (fs *FoldState) UnfoldAtLine(line int) bool {
	for i, r := range fs.regions {
		if !r.Folded {
			continue
		}
		if line >= r.StartLine && line <= r.EndLine {
			fs.regions[i].Folded = false
			return true
		}
	}
	return false
}

// VisibleLines returns which original line indices are visible after folding.
func (fs *FoldState) VisibleLines(totalLines int) []int {
	visible := make([]int, 0, totalLines)
	for i := 0; i < totalLines; i++ {
		if !fs.IsLineHidden(i) {
			visible = append(visible, i)
		}
	}
	return visible
}

// DetectFoldRegions scans text for brace-delimited blocks and returns fold
// regions. This is a simple heuristic for when tree-sitter data is unavailable.
func DetectFoldRegions(text string) []FoldRegion {
	lines := strings.Split(text, "\n")
	var regions []FoldRegion
	var stack []int // stack of opening brace line indices

	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		for _, ch := range trimmed {
			if ch == '{' {
				stack = append(stack, i)
			} else if ch == '}' {
				if len(stack) > 0 {
					start := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if i-start >= 2 { // at least 2 lines between braces
						regions = append(regions, FoldRegion{
							StartLine: start,
							EndLine:   i,
						})
					}
				}
			}
		}
	}

	return regions
}
