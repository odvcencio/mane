package editor

import (
	"reflect"
	"testing"
)

func TestFoldToggle(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 5},
		{StartLine: 10, EndLine: 15},
	})

	if fs.Toggle(0) != true {
		t.Error("Toggle should return true for existing region")
	}
	if !fs.regions[0].Folded {
		t.Error("region 0 should be folded after Toggle")
	}

	if fs.Toggle(0) != true {
		t.Error("Toggle should return true for existing region")
	}
	if fs.regions[0].Folded {
		t.Error("region 0 should be unfolded after second Toggle")
	}

	if fs.Toggle(99) != false {
		t.Error("Toggle should return false for non-existing line")
	}
}

func TestFoldAllUnfoldAll(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 5},
		{StartLine: 10, EndLine: 15},
	})

	fs.FoldAll()
	for _, r := range fs.Regions() {
		if !r.Folded {
			t.Errorf("region at line %d should be folded", r.StartLine)
		}
	}

	fs.UnfoldAll()
	for _, r := range fs.Regions() {
		if r.Folded {
			t.Errorf("region at line %d should be unfolded", r.StartLine)
		}
	}
}

func TestIsLineHidden(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 2, EndLine: 5, Folded: true},
	})

	tests := []struct {
		line   int
		hidden bool
	}{
		{0, false},
		{1, false},
		{2, false}, // start line is visible
		{3, true},
		{4, true},
		{5, true},
		{6, false},
	}
	for _, tt := range tests {
		if got := fs.IsLineHidden(tt.line); got != tt.hidden {
			t.Errorf("IsLineHidden(%d) = %v, want %v", tt.line, got, tt.hidden)
		}
	}
}

func TestSetRegionsPreservesFoldState(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 5},
		{StartLine: 10, EndLine: 15},
	})
	fs.regions[0].Folded = true

	// Update regions with same start lines
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 6},  // same start, different end
		{StartLine: 10, EndLine: 20}, // same start, different end
		{StartLine: 25, EndLine: 30}, // new region
	})

	if !fs.regions[0].Folded {
		t.Error("region at line 0 should preserve folded state")
	}
	if fs.regions[1].Folded {
		t.Error("region at line 10 was not folded, should stay unfolded")
	}
	if fs.regions[2].Folded {
		t.Error("new region at line 25 should not be folded")
	}
}

func TestVisibleLines(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 1, EndLine: 3, Folded: true},
	})

	got := fs.VisibleLines(6)
	want := []int{0, 1, 4, 5} // lines 2,3 are hidden
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VisibleLines = %v, want %v", got, want)
	}
}

func TestFoldAtLine(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 10},
		{StartLine: 2, EndLine: 5}, // inner region
	})

	// Fold at line 3 should fold innermost containing region
	if !fs.FoldAtLine(3) {
		t.Error("FoldAtLine(3) should find a region")
	}
	if !fs.regions[1].Folded {
		t.Error("inner region should be folded")
	}
	if fs.regions[0].Folded {
		t.Error("outer region should not be folded")
	}
}

func TestUnfoldAtLine(t *testing.T) {
	fs := NewFoldState()
	fs.SetRegions([]FoldRegion{
		{StartLine: 0, EndLine: 10, Folded: true},
	})

	if !fs.UnfoldAtLine(5) {
		t.Error("UnfoldAtLine(5) should find a region")
	}
	if fs.regions[0].Folded {
		t.Error("region should be unfolded")
	}
}

func TestDetectFoldRegions(t *testing.T) {
	text := `func main() {
	if true {
		doSomething()
		doMore()
	}
	return
}`
	regions := DetectFoldRegions(text)

	if len(regions) != 2 {
		t.Fatalf("expected 2 fold regions, got %d", len(regions))
	}

	// Inner block: lines 1-4
	found := false
	for _, r := range regions {
		if r.StartLine == 1 && r.EndLine == 4 {
			found = true
		}
	}
	if !found {
		t.Error("expected fold region at lines 1-4")
	}

	// Outer block: lines 0-6
	found = false
	for _, r := range regions {
		if r.StartLine == 0 && r.EndLine == 6 {
			found = true
		}
	}
	if !found {
		t.Error("expected fold region at lines 0-6")
	}
}
