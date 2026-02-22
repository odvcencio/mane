package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
	"github.com/odvcencio/mane/editor"
	"github.com/odvcencio/mane/lsp"
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

func TestExpandSnippetInsert(t *testing.T) {
	text, cursor := expandSnippetInsert("func ${1:name}(${2:args}) {\n\t$0\n}")
	if text != "func name(args) {\n\t\n}" {
		t.Fatalf("expanded text = %q", text)
	}
	if cursor != 5 {
		t.Fatalf("cursor = %d, want 5", cursor)
	}

	text, cursor = expandSnippetInsert("console.log($0)")
	if text != "console.log()" {
		t.Fatalf("expanded text = %q", text)
	}
	if cursor != len([]rune("console.log(")) {
		t.Fatalf("cursor = %d, want %d", cursor, len([]rune("console.log(")))
	}
}

func TestApplyLSPCompletionSnippetRespectsTabStops(t *testing.T) {
	app := newTestAppWithText(t, "")

	app.applyLSPCompletion(lsp.CompletionItem{
		Label:            "fn",
		InsertText:       "func ${1:name}(${2:args}) {\n\t$0\n}",
		InsertTextFormat: 2,
	})

	if got := app.tabs.ActiveBuffer().Text(); got != "func name(args) {\n\t\n}" {
		t.Fatalf("buffer text = %q", got)
	}
	if got := app.textArea.CursorOffset(); got != 5 {
		t.Fatalf("cursor offset = %d, want 5", got)
	}
}

func TestNewManeAppLoadsLSPServerOverrides(t *testing.T) {
	t.Setenv("MANE_LSP_CONFIG", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".mane-lsp.json")
	config := `{
  "go": {"command": "custom-gopls", "args": ["serve"]},
  "python": {"command": "custom-pyright"}
}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write lsp config: %v", err)
	}

	app := newManeApp(dir)
	goCfg, ok := app.lspServers["go"]
	if !ok {
		t.Fatal("missing go server config")
	}
	if goCfg.Command != "custom-gopls" || len(goCfg.Args) != 1 || goCfg.Args[0] != "serve" {
		t.Fatalf("go server override = %+v", goCfg)
	}
	pyCfg, ok := app.lspServers["python"]
	if !ok || pyCfg.Command != "custom-pyright" {
		t.Fatalf("python server override = %+v", pyCfg)
	}
}

func TestHighlightStateDetectFoldRegionsUsesTreeSitter(t *testing.T) {
	hs := newHighlightState()
	if ok := hs.setup("sample.go"); !ok {
		t.Fatal("expected tree-sitter setup for Go")
	}

	text := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar (\n\ta = fmt.Sprintf(\"%s\", os.Args[0])\n\tb = 2\n)\n"
	if got := editor.DetectFoldRegions(text); len(got) != 0 {
		t.Fatalf("heuristic detector unexpectedly found regions: %+v", got)
	}

	_ = hs.highlight([]byte(text))
	regions := hs.detectFoldRegions(text)
	if len(regions) == 0 {
		t.Fatal("expected tree-sitter fold regions for Go source")
	}
}

func TestHighlightStateDetectFoldRegionsFallsBackToHeuristic(t *testing.T) {
	hs := newHighlightState()
	if ok := hs.setup("notes.txt"); ok {
		t.Fatal("unexpected tree-sitter setup for plain text")
	}

	text := "func main() {\n\tif true {\n\t\tprintln(1)\n\t}\n}\n"
	regions := hs.detectFoldRegions(text)
	if len(regions) == 0 {
		t.Fatal("expected heuristic fold regions when tree-sitter is unavailable")
	}
}

func TestHandleGlobalKeyCtrlShiftRightBraceUnfolds(t *testing.T) {
	app := newTestAppWithText(t, "func main() {\n\tprintln(1)\n}\n")
	app.foldState.SetRegions([]editor.FoldRegion{
		{StartLine: 0, EndLine: 2, Folded: true},
	})
	app.textArea.SetCursorPosition(0, 1)

	result := app.handleGlobalKey(runtime.KeyMsg{
		Key:   terminal.KeyRune,
		Ctrl:  true,
		Shift: true,
		Rune:  '}',
	})
	if !result.Handled {
		t.Fatalf("expected key handling for Ctrl+Shift+], got %#v", result)
	}

	regions := app.foldState.Regions()
	if len(regions) != 1 || regions[0].Folded {
		t.Fatalf("expected unfolded region after shortcut, got %+v", regions)
	}
}

func TestFoldCommandsUpdateVisibleLines(t *testing.T) {
	app := newTestAppWithText(t, "func main() {\n\tprintln(1)\n\tprintln(2)\n}\nnext")
	app.foldState.SetRegions([]editor.FoldRegion{
		{StartLine: 0, EndLine: 3},
	})
	app.textArea.SetCursorPosition(0, 0)

	app.cmdFoldAtCursor()
	if got, want := app.textArea.VisibleLines(), []int{0, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible lines after fold = %v, want %v", got, want)
	}

	app.cmdUnfoldAtCursor()
	if got := app.textArea.VisibleLines(); got != nil {
		t.Fatalf("expected all lines visible after unfold, got %v", got)
	}
}

func TestGotoLineUnfoldsHiddenTarget(t *testing.T) {
	app := newTestAppWithText(t, "func main() {\n\tprintln(1)\n\tprintln(2)\n}\nnext")
	app.foldState.SetRegions([]editor.FoldRegion{
		{StartLine: 0, EndLine: 3, Folded: true},
	})
	app.applyFoldVisibility()

	app.onGotoLine(3)
	regions := app.foldState.Regions()
	if len(regions) != 1 || regions[0].Folded {
		t.Fatalf("expected folded region to be unfolded for goto, got %+v", regions)
	}
	_, row := app.textArea.CursorPosition()
	if row != 2 {
		t.Fatalf("cursor row = %d, want 2", row)
	}
}

func TestLspDiagnosticsPaletteNavigatesAndRevealsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	text := "func main() {\n\tprintln(1)\n\tprintln(2)\n}\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	app := newManeApp(dir)
	if err := app.openFile(path); err != nil {
		t.Fatalf("openFile: %v", err)
	}
	app.foldState.SetRegions([]editor.FoldRegion{
		{StartLine: 0, EndLine: 3, Folded: true},
	})
	app.applyFoldVisibility()

	uri := fileURI(path)
	app.lspMu.Lock()
	app.lspDiagnostics[uri] = []lsp.Diagnostic{
		{
			Range: lsp.Range{
				Start: lsp.Position{Line: 2, Character: 1},
				End:   lsp.Position{Line: 2, Character: 8},
			},
			Severity: 1,
			Message:  "test error",
			Source:   "test",
		},
	}
	app.lspMu.Unlock()

	app.cmdLspDiagnostics()
	if app.lspPalette == nil || !app.lspPalette.Open() {
		t.Fatal("expected diagnostics palette to open")
	}
	if got := app.lspPalette.FilteredCount(); got != 1 {
		t.Fatalf("filtered diagnostics = %d, want 1", got)
	}

	result := app.lspPalette.HandleMessage(runtime.KeyMsg{Key: terminal.KeyEnter})
	if !result.Handled {
		t.Fatalf("expected Enter to execute diagnostic command, got %#v", result)
	}
	if app.lspPalette.Open() {
		t.Fatal("expected diagnostics palette closed after selection")
	}
	regions := app.foldState.Regions()
	if len(regions) != 1 || regions[0].Folded {
		t.Fatalf("expected folded region unfolded after selecting diagnostic, got %+v", regions)
	}
	col, row := app.textArea.CursorPosition()
	if row != 2 || col != 1 {
		t.Fatalf("cursor = (%d,%d), want (1,2)", col, row)
	}
}

func TestBreadcrumbsIncludeCurrentSymbolPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	text := "package main\n\nfunc greet() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	app := newManeApp(dir)
	if err := app.openFile(path); err != nil {
		t.Fatalf("openFile: %v", err)
	}

	app.textArea.SetCursorPosition(1, 2)
	app.syncBreadcrumbs()

	found := false
	for _, item := range app.breadcrumbs.Items {
		if item.Label == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected symbol breadcrumb 'greet', got %+v", app.breadcrumbs.Items)
	}
}

func TestHandleGlobalKeyCtrlRightBracketJumpsToMatch(t *testing.T) {
	app := newTestAppWithText(t, "{\n}\n")
	app.textArea.SetCursorOffset(0)
	want, ok := editor.FindMatchingBracket(app.textArea.Text(), 0)
	if !ok {
		t.Fatal("expected matching bracket in test text")
	}

	result := app.handleGlobalKey(runtime.KeyMsg{
		Key:  terminal.KeyRune,
		Ctrl: true,
		Rune: ']',
	})
	if !result.Handled {
		t.Fatalf("expected key handling for Ctrl+], got %#v", result)
	}
	if got := app.textArea.CursorOffset(); got != want {
		t.Fatalf("cursor offset = %d, want %d", got, want)
	}
}

func TestBlockSelectionAltShiftExpansionAndInsert(t *testing.T) {
	app := newTestAppWithText(t, "abc\ndef")
	app.textArea.SetCursorPosition(1, 0)

	result := app.handleGlobalKey(runtime.KeyMsg{
		Key:   terminal.KeyDown,
		Alt:   true,
		Shift: true,
	})
	if !result.Handled {
		t.Fatalf("expected handled for Alt+Shift+Down, got %#v", result)
	}
	if !app.isBlockSelectionMode() {
		t.Fatal("expected block selection mode")
	}

	result = app.handleGlobalKey(runtime.KeyMsg{
		Key:  terminal.KeyRune,
		Rune: 'X',
	})
	if !result.Handled {
		t.Fatalf("expected handled block insert, got %#v", result)
	}
	if app.isBlockSelectionMode() {
		t.Fatal("expected block selection cleared after insert")
	}

	const want = "aXbc\ndXef"
	if got := app.tabs.ActiveBuffer().Text(); got != want {
		t.Fatalf("buffer text = %q, want %q", got, want)
	}
	if got := app.textArea.Text(); got != want {
		t.Fatalf("text area text = %q, want %q", got, want)
	}
}

func TestBlockSelectionDeleteAndBackspace(t *testing.T) {
	app := newTestAppWithText(t, "abcd\nefgh")
	app.blockSelection.Set(0, 1, 1, 3)
	app.syncBlockHighlights()

	result := app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyDelete})
	if !result.Handled {
		t.Fatalf("expected handled block delete, got %#v", result)
	}
	if got := app.tabs.ActiveBuffer().Text(); got != "ad\neh" {
		t.Fatalf("delete result = %q, want %q", got, "ad\neh")
	}

	app = newTestAppWithText(t, "abc\ndef")
	app.blockSelection.Set(0, 1, 2, 2) // zero-width caret block
	app.syncBlockHighlights()

	result = app.handleGlobalKey(runtime.KeyMsg{Key: terminal.KeyBackspace})
	if !result.Handled {
		t.Fatalf("expected handled block backspace, got %#v", result)
	}
	if got := app.tabs.ActiveBuffer().Text(); got != "ac\ndf" {
		t.Fatalf("backspace result = %q, want %q", got, "ac\ndf")
	}
}

func TestSelectionCountForBlockSelection(t *testing.T) {
	app := newTestAppWithText(t, "abcd\nef")
	app.blockSelection.Set(0, 1, 1, 3)

	if got := app.selectionCount(); got != 3 {
		t.Fatalf("selectionCount() with block = %d, want 3", got)
	}
}

func TestMCPAccessMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	app := newManeApp(dir)
	if err := app.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	content, err := app.ReadBuffer(path)
	if err != nil {
		t.Fatalf("ReadBuffer: %v", err)
	}
	if !strings.Contains(content, "func main") {
		t.Fatalf("unexpected buffer content: %q", content)
	}

	if err := app.WriteBuffer(path, "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatalf("WriteBuffer: %v", err)
	}
	if got := app.tabs.ActiveBuffer().Text(); got != "package main\n\nfunc main() {}\n" {
		t.Fatalf("WriteBuffer text = %q", got)
	}

	if err := app.ApplyEdit(path, 2, 12, 2, 14, "x"); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if got := app.tabs.ActiveBuffer().Text(); !strings.Contains(got, "func main() x") {
		t.Fatalf("ApplyEdit text = %q", got)
	}

	tree, err := app.GetSyntaxTree(path)
	if err != nil {
		t.Fatalf("GetSyntaxTree: %v", err)
	}
	if !strings.Contains(tree, "(source_file") {
		t.Fatalf("unexpected syntax tree output: %q", tree)
	}

	symbols, err := app.GetSymbols(path)
	if err != nil {
		t.Fatalf("GetSymbols: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}

	search := app.Search("main")
	if len(search) == 0 {
		t.Fatal("expected search matches")
	}

	if err := app.RunCommand("unknown.command"); err == nil {
		t.Fatal("expected RunCommand error for unknown command")
	}
}
