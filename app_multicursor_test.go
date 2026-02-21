package main

import (
	"strings"
	"testing"

	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
)

func newTestAppWithText(t *testing.T, text string) *maneApp {
	t.Helper()
	app := newManeApp("")
	if app.tabs.NewUntitled() != 0 {
		t.Fatalf("expected first untitled tab index 0")
	}
	buf := app.tabs.ActiveBuffer()
	if buf == nil {
		t.Fatalf("expected active buffer")
	}
	buf.SetText(text)
	app.textArea.SetText(text)
	app.syncMultiCursorFromTextArea()
	return app
}

func setPrimarySelection(app *maneApp, start, end, cursor int) {
	app.textArea.SetSelection(widgets.Selection{Start: start, End: end})
	app.textArea.SetCursorOffset(cursor)
	app.syncMultiCursorFromTextArea()
}

func TestApplyPasteAcrossMultiCursors(t *testing.T) {
	app := newTestAppWithText(t, "foo bar foo")
	setPrimarySelection(app, 0, 3, 3)
	app.multiCursor.AddSelection(8, 11)

	app.applyPaste("X")

	want := "X bar X"
	if got := app.tabs.ActiveBuffer().Text(); got != want {
		t.Fatalf("applyPaste() buffer text = %q, want %q", got, want)
	}
	if got := app.textArea.Text(); got != want {
		t.Fatalf("applyPaste() textArea text = %q, want %q", got, want)
	}
}

func TestApplyPasteFallsBackToSingleCursor(t *testing.T) {
	app := newTestAppWithText(t, "foo bar")
	setPrimarySelection(app, 0, 3, 3)
	if app.isMultiCursorMode() {
		t.Fatalf("expected single cursor mode before paste")
	}

	app.applyPaste("X")

	const want = "X bar"
	if got := app.tabs.ActiveBuffer().Text(); got != want {
		t.Fatalf("applyPaste() buffer text = %q, want %q", got, want)
	}
	sel := app.textArea.GetSelection()
	if !sel.IsEmpty() {
		t.Fatalf("selection not cleared after paste: %+v", sel)
	}
}

func TestApplyDeleteBackspaceAcrossMultiCursors(t *testing.T) {
	app := newTestAppWithText(t, "abcde")
	setPrimarySelection(app, 0, 2, 2)  // "ab"
	app.multiCursor.AddSelection(3, 5) // "de"

	app.applyMultiCursorDeleteBackspace()

	if got := app.tabs.ActiveBuffer().Text(); got != "c" {
		t.Fatalf("applyMultiCursorDeleteBackspace() = %q, want %q", got, "c")
	}
}

func TestApplyDeleteForwardAcrossMultiCursors(t *testing.T) {
	app := newTestAppWithText(t, "abcde")
	// Primary cursor at index 1, second cursor at index 3, both with no selection.
	setPrimarySelection(app, 1, 1, 1)
	app.multiCursor.AddSelection(3, 3)

	app.applyMultiCursorDeleteForward()

	if got := app.tabs.ActiveBuffer().Text(); got != "ace" {
		t.Fatalf("applyMultiCursorDeleteForward() = %q, want %q", got, "ace")
	}
}

func TestHandleGlobalPasteAndMouseReset(t *testing.T) {
	app := newTestAppWithText(t, "foo bar foo")
	setPrimarySelection(app, 0, 3, 3)
	app.multiCursor.AddSelection(8, 11)
	result := app.handleGlobalPaste(runtime.PasteMsg{Text: "X"})
	if !result.Handled {
		t.Fatalf("handleGlobalPaste() = %#v, want Handled", result)
	}
	if got := app.textArea.Text(); got != "X bar X" {
		t.Fatalf("handleGlobalPaste() text = %q, want %q", got, "X bar X")
	}

	result = app.handleGlobalMouse(runtime.MouseMsg{
		Button: runtime.MouseLeft,
		Action: runtime.MousePress,
	})
	if result.Handled {
		t.Fatalf("handleGlobalMouse() = %#v, want Unhandled", result)
	}
	if app.isMultiCursorMode() {
		t.Fatal("expected multicursor mode reset after mouse press")
	}
}

func TestHandleGlobalPasteFallsBackToSingleCursor(t *testing.T) {
	app := newTestAppWithText(t, "foo bar")
	setPrimarySelection(app, 0, 3, 3)
	if app.isMultiCursorMode() {
		t.Fatalf("expected single cursor mode before paste")
	}

	result := app.handleGlobalPaste(runtime.PasteMsg{Text: "X"})
	if !result.Handled {
		t.Fatalf("handleGlobalPaste() = %#v, want Handled", result)
	}
	if got := app.tabs.ActiveBuffer().Text(); got != "X bar" {
		t.Fatalf("handleGlobalPaste() buffer text = %q, want %q", got, "X bar")
	}
}

func TestSelectionCountMergesMultiCursorRanges(t *testing.T) {
	app := newTestAppWithText(t, "abcde fghij")
	// Create overlapping ranges: [1,4] and [3,7] => merged [1,7].
	setPrimarySelection(app, 1, 4, 4)
	app.multiCursor.AddSelection(3, 7)

	if got := app.selectionCount(); got != 6 {
		t.Fatalf("selectionCount() = %d, want 6", got)
	}
}

func TestStatusShowsMultiCursorMetadata(t *testing.T) {
	app := newTestAppWithText(t, "foo foo")
	setPrimarySelection(app, 0, 3, 3)
	app.multiCursor.AddSelection(4, 7)
	app.updateStatus()

	got := app.status.Get()
	if !strings.Contains(got, "Sel 6") {
		t.Fatalf("status missing merged selection count, got %q", got)
	}
	if !strings.Contains(got, "(2 cursors)") {
		t.Fatalf("status missing multi-cursor count, got %q", got)
	}
}

func TestHandleGlobalKeyExitAndStatus(t *testing.T) {
	app := newTestAppWithText(t, "foo foo foo")
	setPrimarySelection(app, 0, 3, 3)
	app.multiCursor.AddSelection(4, 7)
	if result := app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyCtrlD}); !result.Handled {
		t.Fatalf("handleGlobalKey(Ctrl+D) = %#v, want Handled", result)
	}
	if app.multiCursor.Count() != 3 {
		t.Fatalf("expected 3 cursors after first Ctrl+D, got %d", app.multiCursor.Count())
	}

	if result := app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyCtrlD}); !result.Handled {
		t.Fatalf("handleGlobalKey(second Ctrl+D) = %#v, want Handled", result)
	}
	if app.multiCursor.Count() != 3 {
		t.Fatalf("expected 3 cursors after third Ctrl+D, got %d", app.multiCursor.Count())
	}
	if got := app.status.Get(); !strings.Contains(got, "no further occurrences") {
		t.Fatalf("expected status with no further occurrences, got %q", got)
	}

	if result := app.handleGlobalKey(runtime.KeyMsg{
		Key:   terminal.KeyUp,
		Shift: false,
		Ctrl:  false,
		Alt:   false,
	}); result.Handled {
		t.Fatalf("handleGlobalKey(navigation) in multicursor = %#v, expected unhandled", result)
	}
	if app.multiCursor.IsMulti() {
		t.Fatal("expected multi-cursor mode cleared on non-command key")
	}
}

func TestHandleGlobalKeyCtrlDStartsMultiCursor(t *testing.T) {
	app := newTestAppWithText(t, "foo foo foo")
	setPrimarySelection(app, 0, 3, 3)

	if result := app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyCtrlD}); !result.Handled {
		t.Fatalf("handleGlobalKey(Ctrl+D) = %#v, want Handled", result)
	}
	if !app.isMultiCursorMode() {
		t.Fatal("expected multi-cursor mode enabled after first Ctrl+D")
	}
	if got := app.multiCursor.Count(); got != 2 {
		t.Fatalf("multiCursor.Count() = %d, want 2", got)
	}

	if result := app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyCtrlD}); !result.Handled {
		t.Fatalf("handleGlobalKey(second Ctrl+D) = %#v, want Handled", result)
	}
	if got := app.multiCursor.Count(); got != 3 {
		t.Fatalf("multiCursor.Count() = %d, want 3", got)
	}
	if got := app.status.Get(); strings.Contains(got, "no further occurrences") {
		t.Fatal("status should not indicate no occurrences after successful second addition")
	}
}

func TestSwitchTabResetsMultiCursorState(t *testing.T) {
	app := newManeApp("")

	if app.tabs.NewUntitled() != 0 {
		t.Fatalf("expected first tab index 0")
	}
	app.tabs.Buffer(0).SetText("first line")
	if app.tabs.NewUntitled() != 1 {
		t.Fatalf("expected second tab index 1")
	}
	app.tabs.Buffer(1).SetText("second line")

	app.tabs.SetActive(1)
	app.syncTextArea()
	setPrimarySelection(app, 0, 6, 6) // "second"
	app.multiCursor.AddSelection(8, 12)

	app.switchTab(0)
	if app.isMultiCursorMode() {
		t.Fatal("expected multi-cursor mode cleared on tab switch")
	}
	if got := app.textArea.Text(); got != "first line" {
		t.Fatalf("switchTab() text = %q, want %q", got, "first line")
	}
}

func TestCmdCloseTabResetsMultiCursorState(t *testing.T) {
	app := newManeApp("")

	if app.tabs.NewUntitled() != 0 {
		t.Fatalf("expected first tab index 0")
	}
	app.tabs.Buffer(0).SetText("first tab")
	if app.tabs.NewUntitled() != 1 {
		t.Fatalf("expected second tab index 1")
	}
	app.tabs.Buffer(1).SetText("second tab")

	app.tabs.SetActive(0)
	app.syncTextArea()
	setPrimarySelection(app, 0, 5, 5)
	app.multiCursor.AddSelection(6, 9)

	app.cmdCloseTab()
	if app.isMultiCursorMode() {
		t.Fatal("expected multi-cursor mode cleared after closing active tab")
	}

	if got := app.tabs.ActiveBuffer().Text(); got != "second tab" {
		t.Fatalf("active buffer text = %q, want %q", got, "second tab")
	}
	if got := app.textArea.Text(); got != "second tab" {
		t.Fatalf("text area text = %q, want %q", got, "second tab")
	}
}

func TestSelectionCountUsesTextAreaWhenSingleCursor(t *testing.T) {
	app := newTestAppWithText(t, "abcde")
	setPrimarySelection(app, 1, 4, 4)

	app.multiCursor.SetPrimary(0, 0)
	if got := app.selectionCount(); got != 3 {
		t.Fatalf("selectionCount() = %d, want 3", got)
	}
}

func TestApplyPasteAcrossMultiCursorsWithMultilineText(t *testing.T) {
	app := newTestAppWithText(t, "foo bar")
	setPrimarySelection(app, 0, 3, 3)
	app.multiCursor.AddSelection(4, 7)

	app.applyPaste("x\ny")

	if got := app.tabs.ActiveBuffer().Text(); got != "x\ny x\ny" {
		t.Fatalf("applyPaste() = %q, want %q", got, "x\ny x\ny")
	}
}
