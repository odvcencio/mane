package editor

import (
	"reflect"
	"testing"
)

func TestBlockSelectionSetClear(t *testing.T) {
	bs := NewBlockSelection()
	if bs.Active {
		t.Error("should start inactive")
	}

	bs.Set(1, 3, 2, 5)
	if !bs.Active {
		t.Error("should be active after Set")
	}
	if bs.StartLine != 1 || bs.EndLine != 3 || bs.StartCol != 2 || bs.EndCol != 5 {
		t.Error("bounds not set correctly")
	}

	bs.Clear()
	if bs.Active {
		t.Error("should be inactive after Clear")
	}
}

func TestBlockSelectionNormalize(t *testing.T) {
	bs := &BlockSelection{StartLine: 5, EndLine: 2, StartCol: 8, EndCol: 3, Active: true}
	bs.Normalize()
	if bs.StartLine != 2 || bs.EndLine != 5 {
		t.Errorf("lines should be normalized: got %d-%d", bs.StartLine, bs.EndLine)
	}
	if bs.StartCol != 3 || bs.EndCol != 8 {
		t.Errorf("cols should be normalized: got %d-%d", bs.StartCol, bs.EndCol)
	}
}

func TestBlockSelectionExpand(t *testing.T) {
	bs := &BlockSelection{StartLine: 2, EndLine: 4, StartCol: 3, EndCol: 7, Active: true}

	bs.ExpandUp()
	if bs.StartLine != 1 {
		t.Errorf("ExpandUp: StartLine = %d, want 1", bs.StartLine)
	}

	bs.ExpandDown(10)
	if bs.EndLine != 5 {
		t.Errorf("ExpandDown: EndLine = %d, want 5", bs.EndLine)
	}

	bs.ExpandLeft()
	if bs.StartCol != 2 {
		t.Errorf("ExpandLeft: StartCol = %d, want 2", bs.StartCol)
	}

	bs.ExpandRight(20)
	if bs.EndCol != 8 {
		t.Errorf("ExpandRight: EndCol = %d, want 8", bs.EndCol)
	}

	// Test bounds
	bs.StartLine = 0
	bs.ExpandUp()
	if bs.StartLine != 0 {
		t.Error("ExpandUp should not go below 0")
	}

	bs.StartCol = 0
	bs.ExpandLeft()
	if bs.StartCol != 0 {
		t.Error("ExpandLeft should not go below 0")
	}
}

func TestBlockSelectionExtractBlock(t *testing.T) {
	text := "hello world\nfoo bar baz\nalpha beta\nend"
	bs := &BlockSelection{StartLine: 0, EndLine: 2, StartCol: 2, EndCol: 5, Active: true}

	got := bs.ExtractBlock(text)
	want := []string{"llo", "o b", "pha"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractBlock = %v, want %v", got, want)
	}
}

func TestBlockSelectionInsertAtBlock(t *testing.T) {
	text := "hello\nworld\nfoo"
	bs := &BlockSelection{StartLine: 0, EndLine: 2, StartCol: 2, EndCol: 2, Active: true}

	got := bs.InsertAtBlock(text, "XX")
	want := "heXXllo\nwoXXrld\nfoXXo"
	if got != want {
		t.Errorf("InsertAtBlock = %q, want %q", got, want)
	}
}

func TestBlockSelectionDeleteBlock(t *testing.T) {
	text := "hello world\nfoo bar baz\nalpha beta"
	bs := &BlockSelection{StartLine: 0, EndLine: 2, StartCol: 2, EndCol: 5, Active: true}

	got := bs.DeleteBlock(text)
	want := "he world\nfoar baz\nal beta"
	if got != want {
		t.Errorf("DeleteBlock = %q, want %q", got, want)
	}
}

func TestBlockSelectionShortLines(t *testing.T) {
	text := "ab\ncd\nefghij"
	bs := &BlockSelection{StartLine: 0, EndLine: 2, StartCol: 1, EndCol: 4, Active: true}

	got := bs.ExtractBlock(text)
	want := []string{"b", "d", "fgh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractBlock with short lines = %v, want %v", got, want)
	}
}
