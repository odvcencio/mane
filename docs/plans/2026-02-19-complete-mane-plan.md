# Mane Complete Editor Implementation Plan

**Goal:** Transform Mane from a basic text editor into a full-featured IDE with editing power, navigation, LSP, web mode, and MCP extensions.

**Architecture:** Bottom-up approach. Build foundational editor features first (line ops, auto-indent, bracket matching), then navigation UI, then LSP client, then web/MCP modes. Each layer builds on the previous. The `editor/` package handles pure logic, `app.go` bridges to FluffyUI widgets, and new packages (`lsp/`, `web/`, `mcp/`) are added as needed.

**Tech Stack:** Go 1.24, FluffyUI v0.5.2 (TUI framework), gotreesitter (syntax highlighting), LSP over stdio (JSON-RPC 2.0)

**Completion Standard:** Each task is complete only after edits, checks, and a commit are done.

**Execution Rhythm:**
- implement task
- run the task-specific command
- commit immediately

---

## Phase 1: Fix Existing Gaps

### Task 1: Wire Replace UI

The SearchWidget does not have built-in replace. Build a replace widget that embeds SearchWidget behavior and adds replace input + buttons.

**Files:**
- Create: `replace.go` (replace widget)
- Modify: `app.go` (wire replace into app)
- Modify: `commands/commands.go` (wire Replace action)

**Step 1: Build the replace widget**

Create `replace.go` with a widget that renders two rows: search input and replace input, with Replace / Replace All buttons.

```go
package main

import (
	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/widgets"
)

type replaceWidget struct {
	widgets.Base
	searchInput  string
	replaceInput string
	activeField  int // 0=search, 1=replace
	matchCurrent int
	matchTotal   int

	onSearch    func(query string)
	onNext      func()
	onPrev      func()
	onReplace   func(search, replace string)
	onReplaceAll func(search, replace string)
	onClose     func()
}

func newReplaceWidget() *replaceWidget {
	return &replaceWidget{}
}
```

Implement `Measure` (returns Height: 2), `Render` (draws search row and replace row), `HandleMessage` (keyboard input routing to active field, Tab to switch fields, Enter for replace, Ctrl+Enter for replace all, Escape to close, Up/Down for match navigation).

**Step 2: Wire replace into maneApp**

In `app.go`, add:
```go
func (a *maneApp) cmdReplace() runtime.HandleResult {
	a.replaceW.Focus()
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.replaceW})
}

func (a *maneApp) onReplace(search, replace string) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil || len(a.searchMatches) == 0 {
		return
	}
	m := a.searchMatches[a.searchCurrent]
	buf.Replace(search, replace, m)
	a.textArea.SetText(buf.Text())
	// Re-run search to update matches
	a.onSearch(search)
}

func (a *maneApp) onReplaceAll(search, replace string) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	count := buf.ReplaceAll(search, replace)
	a.textArea.SetText(buf.Text())
	a.status.Set(fmt.Sprintf("Replaced %d occurrences", count))
	a.onSearch(search)
}
```

**Step 3: Wire Replace command in palette**

In `commands/commands.go`, the Replace action callback is already in the `Actions` struct but not wired. In `app.go` `run()`, add the Replace callback:

```go
Replace: func() { app.cmdReplace() },
```

**Step 4: Add Ctrl+H keybinding**

In `app.go` globalKeys handler, add:
```go
case terminal.KeyCtrlH:
	return app.cmdReplace()
```

**Step 5: Test manually and commit**

```bash
go build -o mane . && ./mane testfile.go
# Test: Ctrl+H opens replace, type search, type replace, press Enter for replace one, Ctrl+Enter for replace all
```

```bash
git add replace.go app.go commands/commands.go
buckley commit --yes --minimal-output
```

---

### Task 2: Line Operations

Add delete-line, move-line-up/down, and duplicate-line operations.

**Files:**
- Create: `editor/lineops.go`
- Create: `editor/lineops_test.go`
- Modify: `app.go` (add keybindings and commands)
- Modify: `commands/commands.go` (add palette commands)

**Step 1: Write tests for line operations**

```go
package editor

import "testing"

func TestDeleteLine(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		line   int
		want   string
	}{
		{"single line", "hello", 0, ""},
		{"first of two", "hello\nworld", 0, "world"},
		{"second of two", "hello\nworld", 1, "hello"},
		{"middle line", "a\nb\nc", 1, "a\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeleteLine(tt.text, tt.line)
			if got != tt.want {
				t.Errorf("DeleteLine(%q, %d) = %q, want %q", tt.text, tt.line, got, tt.want)
			}
		})
	}
}

func TestMoveLine(t *testing.T) {
	text := "a\nb\nc"
	got := MoveLine(text, 0, 1) // move line 0 down
	if got != "b\na\nc" {
		t.Errorf("MoveLine down = %q, want %q", got, "b\na\nc")
	}
	got = MoveLine(text, 2, -1) // move line 2 up
	if got != "a\nc\nb" {
		t.Errorf("MoveLine up = %q, want %q", got, "a\nc\nb")
	}
}

func TestDuplicateLine(t *testing.T) {
	text := "a\nb\nc"
	got := DuplicateLine(text, 1)
	if got != "a\nb\nb\nc" {
		t.Errorf("DuplicateLine = %q, want %q", got, "a\nb\nb\nc")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestDeleteLine|TestMoveLine|TestDuplicateLine" -v
```

**Step 3: Implement line operations**

Create `editor/lineops.go`:

```go
package editor

import "strings"

// lineRange returns the byte offsets [start, end) of the given 0-based line.
// end includes the trailing newline if present.
func lineRange(text string, line int) (start, end int) {
	cur := 0
	for i := 0; i < line; i++ {
		idx := strings.IndexByte(text[cur:], '\n')
		if idx < 0 {
			return len(text), len(text)
		}
		cur += idx + 1
	}
	start = cur
	idx := strings.IndexByte(text[cur:], '\n')
	if idx < 0 {
		end = len(text)
	} else {
		end = cur + idx + 1
	}
	return
}

// DeleteLine removes the line at the given 0-based line number.
func DeleteLine(text string, line int) string {
	start, end := lineRange(text, line)
	result := text[:start] + text[end:]
	// Remove trailing newline if we deleted the last line and there's a dangling one
	if len(result) > 0 && result[len(result)-1] == '\n' && end == len(text) {
		result = result[:len(result)-1]
	}
	return result
}

// MoveLine moves the line at the given 0-based line number by delta (+1 = down, -1 = up).
func MoveLine(text string, line, delta int) string {
	lines := strings.Split(text, "\n")
	target := line + delta
	if target < 0 || target >= len(lines) {
		return text
	}
	lines[line], lines[target] = lines[target], lines[line]
	return strings.Join(lines, "\n")
}

// DuplicateLine duplicates the line at the given 0-based line number.
func DuplicateLine(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return text
	}
	dup := lines[line]
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:line+1]...)
	result = append(result, dup)
	result = append(result, lines[line+1:]...)
	return strings.Join(result, "\n")
}

// LineCount returns the number of lines in the text.
func LineCount(text string) int {
	if text == "" {
		return 1
	}
	return strings.Count(text, "\n") + 1
}
```

**Step 4: Run tests to verify they pass**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestDeleteLine|TestMoveLine|TestDuplicateLine" -v
```

**Step 5: Wire keybindings in app.go**

In globalKeys handler, add:
```go
case terminal.KeyDelete:
	if key.Ctrl && key.Shift {
		app.cmdDeleteLine()
		return runtime.Handled()
	}
case terminal.KeyUp:
	if key.Alt {
		app.cmdMoveLineUp()
		return runtime.Handled()
	}
case terminal.KeyDown:
	if key.Alt {
		app.cmdMoveLineDown()
		return runtime.Handled()
	}
```

Add methods to maneApp:
```go
func (a *maneApp) cmdDeleteLine() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil { return }
	_, row := a.textArea.CursorPosition()
	text := editor.DeleteLine(buf.Text(), row)
	buf.SetText(text)
	a.textArea.SetText(text)
	a.rehighlight(text)
}

func (a *maneApp) cmdMoveLineUp() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil { return }
	_, row := a.textArea.CursorPosition()
	text := editor.MoveLine(buf.Text(), row, -1)
	buf.SetText(text)
	a.textArea.SetText(text)
	a.textArea.SetCursorPosition(0, row-1)
	a.rehighlight(text)
}

func (a *maneApp) cmdMoveLineDown() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil { return }
	_, row := a.textArea.CursorPosition()
	text := editor.MoveLine(buf.Text(), row, 1)
	buf.SetText(text)
	a.textArea.SetText(text)
	a.textArea.SetCursorPosition(0, row+1)
	a.rehighlight(text)
}

func (a *maneApp) cmdDuplicateLine() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil { return }
	_, row := a.textArea.CursorPosition()
	text := editor.DuplicateLine(buf.Text(), row)
	buf.SetText(text)
	a.textArea.SetText(text)
	a.textArea.SetCursorPosition(0, row+1)
	a.rehighlight(text)
}
```

Add a `rehighlight` helper to maneApp:
```go
func (a *maneApp) rehighlight(text string) {
	ranges := a.highlight.highlight([]byte(text))
	a.applyHighlights(text, ranges)
}
```

**Step 6: Add palette commands**

In `commands/commands.go`, add new actions and commands:
```go
// Add to Actions struct:
DeleteLine    func()
MoveLineUp    func()
MoveLineDown  func()
DuplicateLine func()

// Add to AllCommands:
{ID: "edit.deleteLine", Label: "Delete Line", Shortcut: "Ctrl+Shift+K", Category: "Edit", OnExecute: a.DeleteLine},
{ID: "edit.moveLineUp", Label: "Move Line Up", Shortcut: "Alt+Up", Category: "Edit", OnExecute: a.MoveLineUp},
{ID: "edit.moveLineDown", Label: "Move Line Down", Shortcut: "Alt+Down", Category: "Edit", OnExecute: a.MoveLineDown},
{ID: "edit.duplicateLine", Label: "Duplicate Line", Shortcut: "Ctrl+Shift+D", Category: "Edit", OnExecute: a.DuplicateLine},
```

**Step 7: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/lineops.go editor/lineops_test.go app.go commands/commands.go
buckley commit --yes --minimal-output
```

---

### Task 3: Auto-Indent

When Enter is pressed, maintain the indentation level of the current line. Increase indent after `{`, `(`, `[`.

**Files:**
- Create: `editor/indent.go`
- Create: `editor/indent_test.go`
- Modify: `app.go` (intercept Enter key)

**Step 1: Write tests**

```go
package editor

import "testing"

func TestComputeIndent(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"no indent", "hello", ""},
		{"tab indent", "\thello", "\t"},
		{"space indent", "    hello", "    "},
		{"after open brace", "\tif x {", "\t\t"},
		{"after open paren", "    func(", "        "},
		{"empty line", "", ""},
		{"only whitespace", "    ", "    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeIndent(tt.line)
			if got != tt.want {
				t.Errorf("ComputeIndent(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestDetectIndentStyle(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"tabs", "func main() {\n\tfmt.Println()\n}", "\t"},
		{"spaces", "def main():\n    print()\n", "    "},
		{"mixed prefers tabs", "\ta\n    b\n\tc\n", "\t"},
		{"no indent", "a\nb\nc\n", "\t"}, // default to tab
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIndentStyle(tt.text)
			if got != tt.want {
				t.Errorf("DetectIndentStyle(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify failure**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestComputeIndent|TestDetectIndentStyle" -v
```

**Step 3: Implement**

Create `editor/indent.go`:

```go
package editor

import "strings"

// DetectIndentStyle looks at the text to determine whether tabs or spaces are
// used for indentation. Returns the indent unit string (e.g., "\t" or "    ").
func DetectIndentStyle(text string) string {
	tabs, spaces := 0, 0
	for _, line := range strings.Split(text, "\n") {
		if len(line) == 0 {
			continue
		}
		if line[0] == '\t' {
			tabs++
		} else if line[0] == ' ' {
			spaces++
		}
	}
	if spaces > tabs {
		// Count common space prefix width
		minSpaces := 0
		for _, line := range strings.Split(text, "\n") {
			if len(line) == 0 || line[0] != ' ' {
				continue
			}
			n := 0
			for _, ch := range line {
				if ch != ' ' {
					break
				}
				n++
			}
			if minSpaces == 0 || n < minSpaces {
				minSpaces = n
			}
		}
		if minSpaces <= 0 {
			minSpaces = 4
		}
		return strings.Repeat(" ", minSpaces)
	}
	return "\t"
}

// ComputeIndent returns the indentation string to use for a new line after
// the given line. It copies the existing indent and increases it if the line
// ends with an opening bracket.
func ComputeIndent(line string) string {
	// Extract leading whitespace
	indent := ""
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			indent += string(ch)
		} else {
			break
		}
	}

	// Check if line ends with an opening bracket (ignoring trailing whitespace)
	trimmed := strings.TrimRight(line, " \t")
	if len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last == '{' || last == '(' || last == '[' {
			// Determine indent unit from existing indent
			if strings.Contains(indent, "\t") || indent == "" {
				indent += "\t"
			} else {
				// Count spaces in current indent to determine unit
				indent += "    "
			}
		}
	}

	return indent
}
```

**Step 4: Run tests**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestComputeIndent|TestDetectIndentStyle" -v
```

**Step 5: Wire into app.go**

The TextArea handles Enter key internally. We need to intercept the `OnChange` callback and detect when a newline was just inserted, then inject the indent. Alternatively, we can use the globalKeys to intercept Enter before it reaches TextArea.

The cleanest approach: intercept in `OnChange`. Compare old text to new text; if a single `\n` was inserted, determine indent and inject it.

Add to `newManeApp` in the `SetOnChange` callback:
```go
app.textArea.SetOnChange(func(text string) {
	buf := app.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	oldText := buf.Text()
	buf.SetText(text)
	app.updateStatus()

	// Auto-indent: if a newline was just inserted, add indent
	if len(text) > len(oldText) {
		diff := text[len(oldText)-1:] // naive: check if newline was added
		// Better: find cursor position and check if char before cursor is \n
		offset := app.textArea.CursorOffset()
		if offset > 0 && offset <= len([]rune(text)) {
			runes := []rune(text)
			if runes[offset-1] == '\n' {
				// Find the line above the cursor
				lineAbove := ""
				byteOffset := 0
				for i, r := range runes[:offset-1] {
					_ = i
					byteOffset += len(string(r))
				}
				lineStart := strings.LastIndex(text[:byteOffset], "\n")
				if lineStart < 0 {
					lineStart = 0
				} else {
					lineStart++
				}
				lineAbove = text[lineStart:byteOffset]
				indent := editor.ComputeIndent(lineAbove)
				if indent != "" {
					// Insert indent after the newline
					runes = append(runes[:offset], append([]rune(indent), runes[offset:]...)...)
					newText := string(runes)
					buf.SetText(newText)
					app.textArea.SetText(newText)
					app.textArea.SetCursorOffset(offset + len([]rune(indent)))
				}
			}
		}
	}

	app.highlight.scheduleHighlight([]byte(text), func(ranges []gotreesitter.HighlightRange) {
		app.applyHighlights(text, ranges)
	})
})
```

Note: The auto-indent logic in OnChange is tricky because it triggers recursively. A better approach is to track whether we're inside an auto-indent insertion and skip the callback in that case. Add a `suppressChange bool` field to maneApp.

**Step 6: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/indent.go editor/indent_test.go app.go
buckley commit --yes --minimal-output
```

---

### Task 4: Bracket Matching

Highlight the matching bracket when cursor is adjacent to a bracket character.

**Files:**
- Create: `editor/brackets.go`
- Create: `editor/brackets_test.go`
- Modify: `app.go` (highlight matching bracket on cursor move)

**Step 1: Write tests**

```go
package editor

import "testing"

func TestFindMatchingBracket(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		pos    int
		wantPos int
		wantOk  bool
	}{
		{"open paren", "a(b)", 1, 3, true},
		{"close paren", "a(b)", 3, 1, true},
		{"nested", "((a))", 0, 4, true},
		{"open brace", "{x}", 0, 2, true},
		{"open bracket", "[x]", 0, 2, true},
		{"no match", "a(b", 1, 0, false},
		{"not a bracket", "abc", 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, ok := FindMatchingBracket(tt.text, tt.pos)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && pos != tt.wantPos {
				t.Errorf("pos = %d, want %d", pos, tt.wantPos)
			}
		})
	}
}
```

**Step 2: Run tests to verify failure**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestFindMatchingBracket" -v
```

**Step 3: Implement**

Create `editor/brackets.go`:

```go
package editor

// bracketPairs maps opening brackets to closing brackets and vice versa.
var bracketPairs = map[rune]rune{
	'(': ')', ')': '(',
	'{': '}', '}': '{',
	'[': ']', ']': '[',
}

var openers = map[rune]bool{'(': true, '{': true, '[': true}

// FindMatchingBracket finds the matching bracket for the bracket at the given
// rune position. Returns the rune position of the match and true, or 0 and
// false if no match is found or the position is not a bracket.
func FindMatchingBracket(text string, pos int) (int, bool) {
	runes := []rune(text)
	if pos < 0 || pos >= len(runes) {
		return 0, false
	}
	ch := runes[pos]
	match, ok := bracketPairs[ch]
	if !ok {
		return 0, false
	}

	if openers[ch] {
		// Scan forward
		depth := 1
		for i := pos + 1; i < len(runes); i++ {
			if runes[i] == ch {
				depth++
			} else if runes[i] == match {
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	} else {
		// Scan backward
		depth := 1
		for i := pos - 1; i >= 0; i-- {
			if runes[i] == ch {
				depth++
			} else if runes[i] == match {
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	}
	return 0, false
}
```

**Step 4: Run tests**

```bash
cd /home/draco/work/mane && go test ./editor/ -run "TestFindMatchingBracket" -v
```

**Step 5: Wire into app.go**

Add bracket highlight logic. On cursor position change (tracked via OnChange or a position callback), check if cursor is adjacent to a bracket, find its match, and add highlight entries.

Add a field to maneApp:
```go
bracketHighlights []widgets.TextAreaHighlight
```

Add method:
```go
func (a *maneApp) updateBracketMatch() {
	text := a.textArea.Text()
	offset := a.textArea.CursorOffset()
	runes := []rune(text)

	a.bracketHighlights = nil

	bracketStyle := backend.DefaultStyle().Background(backend.ColorRGB(0x44, 0x44, 0x44))

	// Check character at cursor and before cursor
	for _, pos := range []int{offset, offset - 1} {
		if pos < 0 || pos >= len(runes) {
			continue
		}
		matchPos, ok := editor.FindMatchingBracket(text, pos)
		if ok {
			a.bracketHighlights = []widgets.TextAreaHighlight{
				{Start: pos, End: pos + 1, Style: bracketStyle},
				{Start: matchPos, End: matchPos + 1, Style: bracketStyle},
			}
			break
		}
	}

	a.mergeAllHighlights()
}
```

Add a `mergeAllHighlights` method that combines syntax + search + bracket highlights:
```go
func (a *maneApp) mergeAllHighlights() {
	var merged []widgets.TextAreaHighlight
	merged = append(merged, a.syntaxHighlights...)
	merged = append(merged, a.bracketHighlights...)
	// Search highlights on top (if active)
	if len(a.searchMatches) > 0 {
		// ... add search highlights (refactor from applySearchHighlights)
	}
	a.textArea.SetHighlights(merged)
}
```

Call `updateBracketMatch()` from the OnChange callback after updating status.

**Step 6: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/brackets.go editor/brackets_test.go app.go
buckley commit --yes --minimal-output
```

---

### Task 5: Code Folding

Use tree-sitter node types to identify foldable regions (function bodies, blocks, etc.) and allow collapsing them.

**Files:**
- Create: `editor/folding.go`
- Create: `editor/folding_test.go`
- Modify: `app.go` (fold state, rendering, keybindings)

**Step 1: Implement fold region detection**

```go
package editor

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

func NewFoldState() *FoldState {
	return &FoldState{}
}

// SetRegions replaces the fold regions (from tree-sitter parse).
func (fs *FoldState) SetRegions(regions []FoldRegion) {
	// Preserve fold state for regions that match by start line
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

// ApplyFolding takes full text and returns the visible text with folded
// regions collapsed. Also returns a line mapping from visible to original.
func (fs *FoldState) ApplyFolding(text string) (visible string, lineMap []int) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !fs.IsLineHidden(i) {
			visible += line
			if i < len(lines)-1 {
				visible += "\n"
			}
			lineMap = append(lineMap, i)
		}
	}
	return
}
```

**Step 2: Wire into highlighting**

After tree-sitter parse, walk the tree to identify function/block nodes and create fold regions. This requires access to the parse tree structure from gotreesitter.

In `app.go`, after highlighting, extract fold regions:
```go
func (a *maneApp) updateFoldRegions(text string) {
	if a.highlight.tree == nil {
		return
	}
	// Walk tree to find block-like nodes
	regions := extractFoldRegions(a.highlight.tree, text)
	a.foldState.SetRegions(regions)
}
```

**Step 3: Add keybindings**

- Ctrl+Shift+[ : Fold at cursor
- Ctrl+Shift+] : Unfold at cursor
- Ctrl+K Ctrl+0 : Fold all
- Ctrl+K Ctrl+J : Unfold all

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/folding.go editor/folding_test.go app.go
buckley commit --yes --minimal-output
```

---

### Task 6: Word Wrap Toggle

**Files:**
- Modify: `app.go` (add toggle state and keybinding)
- Modify: `commands/commands.go` (add palette command)

FluffyUI's TextArea doesn't have built-in word wrap. Implement wrap by introducing a render adapter that keeps canonical text separate from display text.

**Step 1: Add toggle**

In maneApp, add:
```go
wordWrap bool
```

Add command:
```go
func (a *maneApp) cmdToggleWordWrap() {
	a.wordWrap = !a.wordWrap
	if a.wordWrap {
		a.status.Set("Word wrap: on")
	} else {
		a.status.Set("Word wrap: off")
	}
		// TODO: Apply wrapped view transform when TextArea API support is added
	}
```

Wire Ctrl+Alt+W keybinding and palette command.

**Step 2: Implement wrapped view adapter**

- keep source text as single-string canonical text
- build a view text map for wrapped display
- translate cursor offsets and highlights between source and view coordinates
- update status bar with current wrap width

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add app.go commands/commands.go
buckley commit --yes --minimal-output
```

---

## Phase 2: Navigation & UI

### Task 7: Go-to-Line Dialog

**Files:**
- Create: `gotoline.go` (go-to-line overlay widget)
- Modify: `app.go` (wire Ctrl+G and command)
- Modify: `commands/commands.go`

**Step 1: Build go-to-line widget**

A simple overlay with an Input widget. Enter jumps to the line, Escape closes.

```go
package main

import (
	"strconv"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/widgets"
	"github.com/odvcencio/fluffyui/backend"
)

type goToLineWidget struct {
	widgets.Base
	input   string
	onGo    func(line int)
	onClose func()
}

func newGoToLineWidget() *goToLineWidget {
	return &goToLineWidget{}
}
```

Implement Measure (1 row), Render (prompt "Go to line: " + input), HandleMessage (rune input, Enter to submit, Escape to close, Backspace).

On Enter:
```go
line, err := strconv.Atoi(g.input)
if err == nil && g.onGo != nil {
	g.onGo(line)
}
```

**Step 2: Wire into app**

```go
func (a *maneApp) cmdGoToLine() runtime.HandleResult {
	a.goToLine.input = ""
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.goToLine})
}
```

Set `onGo` callback:
```go
app.goToLine.onGo = func(line int) {
	if line < 1 { line = 1 }
	app.textArea.SetCursorPosition(0, line-1) // 0-based
	app.updateStatus()
}
```

Wire Ctrl+G in globalKeys. Add to palette commands.

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add gotoline.go app.go commands/commands.go
buckley commit --yes --minimal-output
```

---

### Task 8: Fuzzy File Finder

**Files:**
- Create: `filefinder.go`
- Modify: `app.go`

**Step 1: Build file finder**

Reuse the CommandPalette pattern: walk the directory tree and populate commands dynamically. Or build a custom overlay with fuzzy matching.

Better approach: Use a separate CommandPalette instance dedicated to files. On Ctrl+P, check if already in palette mode; if so, toggle. Otherwise, dynamically build a file list.

Actually, the cleanest approach: Modify the existing Ctrl+P behavior. If the user types a path-like query (contains `/` or `.`), switch to file mode. Otherwise, show commands.

Simplest approach: A dedicated file picker widget.

```go
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/widgets"
)

type fileFinder struct {
	widgets.Base
	root    string
	query   string
	files   []string // all files (relative paths)
	matches []string // filtered matches
	selected int
	onOpen  func(path string)
	onClose func()
	loaded  bool
}

func newFileFinder(root string) *fileFinder {
	return &fileFinder{root: root}
}

func (f *fileFinder) loadFiles() {
	if f.loaded {
		return
	}
	f.loaded = true
	filepath.WalkDir(f.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(f.root, path)
		f.files = append(f.files, rel)
		return nil
	})
	sort.Strings(f.files)
	f.matches = f.files
}
```

Implement fuzzy matching, rendering (show list of matched files), keyboard handling (Up/Down to navigate, Enter to open, Escape to close, typing updates query).

**Step 2: Wire Ctrl+P to file finder**

Remap: Ctrl+P opens file finder, Ctrl+Shift+P opens command palette (or vice versa).

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add filefinder.go app.go
buckley commit --yes --minimal-output
```

---

### Task 9: Breadcrumbs

**Files:**
- Modify: `app.go` (add Breadcrumb widget between tab bar and editor)

**Step 1: Add breadcrumb**

```go
app.breadcrumb = widgets.NewBreadcrumb()
app.breadcrumb.SetSeparator(" > ")
```

Update `syncBreadcrumb()` method called when switching tabs/files:
```go
func (a *maneApp) syncBreadcrumb() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	path := buf.Path()
	if path == "" {
		a.breadcrumb = widgets.NewBreadcrumb(widgets.BreadcrumbItem{Label: "untitled"})
		return
	}
	// Split path into segments
	rel, err := filepath.Rel(a.treeRoot, path)
	if err != nil {
		rel = path
	}
	parts := strings.Split(rel, string(filepath.Separator))
	items := make([]widgets.BreadcrumbItem, len(parts))
	for i, part := range parts {
		items[i] = widgets.BreadcrumbItem{Label: part}
	}
	a.breadcrumb = widgets.NewBreadcrumb(items...)
}
```

Add to layout:
```go
layout := fluffy.VFlex(
	fluffy.Fixed(app.tabBar),
	fluffy.Fixed(app.breadcrumb),
	fluffy.Expanded(app.slot),
	fluffy.Fixed(statusBar),
)
```

**Step 2: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add app.go
buckley commit --yes --minimal-output
```

---

### Task 10: Enhanced Status Bar

**Files:**
- Modify: `app.go` (updateStatus function)

**Step 1: Enhance status bar info**

Update `updateStatus` to show more info:

```go
func (a *maneApp) updateStatus() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		a.status.Set(" untitled")
		return
	}

	dirty := ""
	if buf.Dirty() {
		dirty = " [modified]"
	}

	langName := ""
	if entry := grammars.DetectLanguage(filepath.Base(buf.Path())); entry != nil {
		langName = strings.ToUpper(entry.Name)
	}

	col, row := a.textArea.CursorPosition()

	// Detect line ending
	text := buf.Text()
	lineEnding := "LF"
	if strings.Contains(text, "\r\n") {
		lineEnding = "CRLF"
	}

	// Detect indent style
	indentStyle := "Tabs"
	if editor.DetectIndentStyle(text) != "\t" {
		indentStyle = "Spaces"
	}

	// Selection info
	selInfo := ""
	if sel := a.textArea.GetSelectedText(); sel != "" {
		lines := strings.Count(sel, "\n") + 1
		selInfo = fmt.Sprintf("  (%d selected, %d lines)", len([]rune(sel)), lines)
	}

	a.status.Set(fmt.Sprintf(" %s%s  %s  %s  %s  Ln %d, Col %d%s",
		buf.Title(), dirty, langName, lineEnding, indentStyle, row+1, col+1, selInfo))
}
```

**Step 2: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add app.go
buckley commit --yes --minimal-output
```

---

## Phase 3: LSP Client

### Task 11: LSP Client Core

Build a Go package that manages LSP server lifecycle and JSON-RPC 2.0 communication.

**Files:**
- Create: `lsp/client.go`
- Create: `lsp/protocol.go` (LSP message types)
- Create: `lsp/client_test.go`

**Step 1: Define LSP protocol types**

```go
package lsp

// Position in a text document (0-based line and character).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location links a Range to a URI.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a text document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem represents a text document transferred from client to server.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// CompletionItem represents a completion suggestion.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// Diagnostic represents a compiler error or warning.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// TextEdit represents a change to a text document.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Hover result
type Hover struct {
	Contents string `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}
```

**Step 2: Build JSON-RPC client**

```go
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client manages communication with an LSP server.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	nextID  atomic.Int64
	pending map[int64]chan json.RawMessage
	notify  func(method string, params json.RawMessage)
}

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *int64           `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewClient starts the LSP server process and returns a Client.
func NewClient(ctx context.Context, command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int64]chan json.RawMessage),
	}

	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}
		if msg.ID != nil {
			// Response to a request
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- msg.Result
			}
		} else if msg.Method != "" {
			// Server notification
			if c.notify != nil {
				c.notify(msg.Method, msg.Params)
			}
		}
	}
}

func (c *Client) readMessage() (jsonrpcResponse, error) {
	// Read Content-Length header
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return jsonrpcResponse{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, _ = strconv.Atoi(val)
		}
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return jsonrpcResponse{}, err
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return jsonrpcResponse{}, err
	}
	return resp, nil
}

func (c *Client) sendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

// Call sends a request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.sendMessage(req); err != nil {
		return nil, err
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params interface{}) error {
	return c.sendMessage(jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

// Close shuts down the LSP server.
func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}
```

**Step 3: Implement Initialize handshake**

```go
// Initialize sends the initialize request to the LSP server.
func (c *Client) Initialize(ctx context.Context, rootURI string) error {
	params := map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"completion": map[string]interface{}{
					"completionItem": map[string]interface{}{
						"snippetSupport": true,
					},
				},
				"hover":      map[string]interface{}{},
				"definition": map[string]interface{}{},
				"references": map[string]interface{}{},
				"rename":     map[string]interface{}{},
				"codeAction": map[string]interface{}{},
				"publishDiagnostics": map[string]interface{}{},
			},
		},
	}
	_, err := c.Call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	return c.Notify("initialized", map[string]interface{}{})
}
```

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build ./lsp/
git add lsp/
buckley commit --yes --minimal-output
```

---

### Task 12: LSP Document Sync

**Files:**
- Modify: `lsp/client.go` (add document sync methods)
- Modify: `app.go` (send didOpen/didChange/didSave/didClose)

**Step 1: Add document lifecycle methods**

```go
func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	return c.Notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

func (c *Client) DidChange(uri string, version int, text string) error {
	return c.Notify("textDocument/didChange", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]interface{}{
			{"text": text},
		},
	})
}

func (c *Client) DidSave(uri string) error {
	return c.Notify("textDocument/didSave", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
	})
}

func (c *Client) DidClose(uri string) error {
	return c.Notify("textDocument/didClose", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
	})
}
```

**Step 2: Wire into app.go**

Add an LSP client field to maneApp and connect it during file operations:
- `openFile`: send didOpen
- `OnChange`: debounced didChange
- `cmdSaveFile`: send didSave
- `cmdCloseTab`: send didClose

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add lsp/ app.go
buckley commit --yes --minimal-output
```

---

### Task 13: LSP Server Configuration

**Files:**
- Create: `lsp/servers.go`

**Step 1: Define server config**

```go
package lsp

// ServerConfig maps language IDs to LSP server commands.
type ServerConfig struct {
	Command string
	Args    []string
}

// DefaultServers returns built-in server configurations.
func DefaultServers() map[string]ServerConfig {
	return map[string]ServerConfig{
		"go":         {Command: "gopls"},
		"typescript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"javascript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"python":     {Command: "pyright-langserver", Args: []string{"--stdio"}},
		"rust":       {Command: "rust-analyzer"},
		"c":          {Command: "clangd"},
		"cpp":        {Command: "clangd"},
		"java":       {Command: "jdtls"},
		"lua":        {Command: "lua-language-server"},
		"json":       {Command: "vscode-json-language-server", Args: []string{"--stdio"}},
	}
}
```

**Step 2: Build and commit**

```bash
cd /home/draco/work/mane && go build ./lsp/
git add lsp/servers.go
buckley commit --yes --minimal-output
```

---

### Task 14: LSP Autocomplete

**Files:**
- Create: `completion.go` (completion popup widget)
- Modify: `lsp/client.go` (add Completion method)
- Modify: `app.go` (trigger completion, display popup)

**Step 1: Add completion request to LSP client**

```go
func (c *Client) Completion(ctx context.Context, uri string, pos Position) ([]CompletionItem, error) {
	result, err := c.Call(ctx, "textDocument/completion", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}
	// Handle both CompletionList and []CompletionItem responses
	var items []CompletionItem
	if err := json.Unmarshal(result, &items); err != nil {
		var list struct {
			Items []CompletionItem `json:"items"`
		}
		if err := json.Unmarshal(result, &list); err != nil {
			return nil, err
		}
		items = list.Items
	}
	return items, nil
}
```

**Step 2: Build completion popup widget**

Create `completion.go` - a floating list widget that shows completion items near the cursor position. Uses FluffyUI overlay system.

**Step 3: Wire trigger**

Trigger completion on Ctrl+Space or when typing after `.`:
```go
// In OnChange, after debounce:
if a.lspClient != nil {
	lastChar := ... // detect last typed character
	if lastChar == '.' {
		go a.requestCompletion()
	}
}
```

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add completion.go lsp/client.go app.go
buckley commit --yes --minimal-output
```

---

### Task 15: LSP Diagnostics

**Files:**
- Modify: `lsp/client.go` (handle publishDiagnostics notification)
- Modify: `app.go` (display diagnostics as highlights and in status)

**Step 1: Handle diagnostics notification**

In the LSP client's notification handler:
```go
c.notify = func(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		var diag struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		json.Unmarshal(params, &diag)
		if c.onDiagnostics != nil {
			c.onDiagnostics(diag.URI, diag.Diagnostics)
		}
	}
}
```

**Step 2: Display diagnostics**

In app.go, add diagnostic highlights (red underline for errors, yellow for warnings):
```go
func (a *maneApp) onDiagnostics(uri string, diags []lsp.Diagnostic) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil || "file://"+buf.Path() != uri {
		return
	}
	a.diagnosticHighlights = nil
	for _, d := range diags {
		style := backend.DefaultStyle()
		if d.Severity == 1 { // Error
			style = style.Foreground(backend.ColorRed)
		} else if d.Severity == 2 { // Warning
			style = style.Foreground(backend.ColorYellow)
		}
		// Convert LSP position to rune offset
		start := a.lspPositionToOffset(d.Range.Start)
		end := a.lspPositionToOffset(d.Range.End)
		a.diagnosticHighlights = append(a.diagnosticHighlights, widgets.TextAreaHighlight{
			Start: start, End: end, Style: style,
		})
	}
	a.mergeAllHighlights()
}
```

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add lsp/client.go app.go
buckley commit --yes --minimal-output
```

---

### Task 16: LSP Go-to-Definition, References, Hover

**Files:**
- Modify: `lsp/client.go` (add Definition, References, Hover methods)
- Modify: `app.go` (wire F12, Shift+F12, hover logic)

**Step 1: Add LSP methods**

```go
func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	result, err := c.Call(ctx, "textDocument/definition", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}
	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		var single Location
		if err := json.Unmarshal(result, &single); err != nil {
			return nil, err
		}
		locations = []Location{single}
	}
	return locations, nil
}

func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	result, err := c.Call(ctx, "textDocument/references", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
		"context":      map[string]interface{}{"includeDeclaration": true},
	})
	if err != nil {
		return nil, err
	}
	var locations []Location
	json.Unmarshal(result, &locations)
	return locations, nil
}

func (c *Client) HoverInfo(ctx context.Context, uri string, pos Position) (*Hover, error) {
	result, err := c.Call(ctx, "textDocument/hover", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	var hover Hover
	json.Unmarshal(result, &hover)
	return &hover, nil
}
```

**Step 2: Wire keybindings**

- F12: Go to definition → open file at location
- Shift+F12: Find references → show in a references panel
- Mouse hover: Show hover info tooltip (debounced)

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add lsp/client.go app.go
buckley commit --yes --minimal-output
```

---

### Task 17: LSP Rename and Code Actions

**Files:**
- Modify: `lsp/client.go`
- Create: `renamedialog.go` (rename input overlay)
- Modify: `app.go`

**Step 1: Add Rename and CodeAction methods**

```go
func (c *Client) Rename(ctx context.Context, uri string, pos Position, newName string) (map[string][]TextEdit, error) {
	result, err := c.Call(ctx, "textDocument/rename", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
		"newName":      newName,
	})
	if err != nil {
		return nil, err
	}
	var wsEdit struct {
		Changes map[string][]TextEdit `json:"changes"`
	}
	json.Unmarshal(result, &wsEdit)
	return wsEdit.Changes, nil
}

func (c *Client) CodeAction(ctx context.Context, uri string, rng Range, diagnostics []Diagnostic) ([]CodeAction, error) {
	result, err := c.Call(ctx, "textDocument/codeAction", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"range":        rng,
		"context": map[string]interface{}{
			"diagnostics": diagnostics,
		},
	})
	if err != nil {
		return nil, err
	}
	var actions []CodeAction
	json.Unmarshal(result, &actions)
	return actions, nil
}
```

**Step 2: Build rename dialog**

Similar to go-to-line widget: input overlay for F2 rename. On Enter, sends rename request and applies workspace edits.

**Step 3: Wire F2 and Ctrl+.**

- F2: Open rename dialog with current symbol pre-filled
- Ctrl+.: Show code actions in a command palette-like list

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add lsp/client.go renamedialog.go app.go
buckley commit --yes --minimal-output
```

---

## Phase 4: Web Mode

### Task 18: FluffyUI Web Backend

**Files:**
- Modify: `main.go` (already wired via WithWebServer)

**Step 1: Verify FluffyUI web mode works**

The `-web` flag already passes `fluffy.WithWebServer(*web)`. Test it:

```bash
cd /home/draco/work/mane && go build -o mane . && ./mane -web :8080 .
# Open browser to http://localhost:8080
```

If it works, this task is done. If not, debug the FluffyUI integration.

**Step 2: Commit if any fixes needed**

```bash
git add main.go
buckley commit --yes --minimal-output
```

---

### Task 19: Custom Web Frontend - Backend API

**Files:**
- Create: `web/server.go` (HTTP + WebSocket server)
- Create: `web/api.go` (JSON-RPC API handlers)

**Step 1: Build WebSocket API server**

```go
package web

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/odvcencio/mane/editor"
)

type Server struct {
	tabs     *editor.TabManager
	upgrader websocket.Upgrader
}

func NewServer(tabs *editor.TabManager) *Server {
	return &Server{
		tabs: tabs,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.handleWebSocket(w, r)
		return
	}
	// Serve static files
	http.FileServer(http.FS(webFS)).ServeHTTP(w, r)
}
```

Define API methods: openFile, readBuffer, writeBuffer, applyEdit, listFiles, save, getHighlights, etc.

**Step 2: Build and commit**

```bash
cd /home/draco/work/mane && go build ./web/
git add web/
buckley commit --yes --minimal-output
```

---

### Task 20: Custom Web Frontend - UI

**Files:**
- Create: `web/static/index.html`
- Create: `web/static/app.js`
- Create: `web/static/style.css`
- Create: `web/embed.go` (embed static files)

**Step 1: Build web frontend**

Use Monaco Editor for the editing surface. Build a minimal SPA with:
- File tree sidebar
- Tab bar
- Status bar
- Command palette (Ctrl+P)
- WebSocket connection to backend API

**Step 2: Wire -webui flag**

In `main.go`, add `-webui` flag that uses the custom web server instead of FluffyUI's:
```go
if *webUI != "" {
	webServer := web.NewServer(tabs)
	return http.ListenAndServe(*webUI, webServer)
}
```

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add web/ main.go
buckley commit --yes --minimal-output
```

---

## Phase 5: MCP Extensions

### Task 21: MCP Editor Tools

**Files:**
- Create: `mcptools/tools.go`
- Modify: `app.go` (register MCP tools)

**Step 1: Define MCP tools**

FluffyUI's MCP system likely provides a way to register custom tools. Create editor-specific tools:

```go
package mcptools

import "github.com/odvcencio/mane/editor"

type EditorTools struct {
	tabs *editor.TabManager
	app  interface{} // reference to maneApp for UI operations
}

func NewEditorTools(tabs *editor.TabManager) *EditorTools {
	return &EditorTools{tabs: tabs}
}

// Tool implementations:
// - mane_open_file: opens a file in the editor
// - mane_read_buffer: reads current buffer contents
// - mane_write_buffer: replaces buffer contents
// - mane_apply_edit: applies a text edit at a range
// - mane_search: searches across open files
// - mane_go_to_line: navigates to a specific line
// - mane_get_diagnostics: gets LSP diagnostics
// - mane_run_command: executes a palette command
```

**Step 2: Register with FluffyUI MCP**

Wire the tools into the FluffyUI MCP system when `-mcp` flag is used.

**Step 3: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add mcptools/ app.go
buckley commit --yes --minimal-output
```

---

### Task 22: MCP Code Intelligence Resources

**Files:**
- Modify: `mcptools/tools.go` (add resource handlers)

**Step 1: Implement resource providers**

```go
// Resources:
// - mane://file/{path}: file contents
// - mane://syntax-tree/{path}: tree-sitter parse tree as text
// - mane://symbols/{path}: extracted symbols (functions, types, variables)
// - mane://diagnostics/{path}: LSP diagnostics for the file
```

**Step 2: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add mcptools/
buckley commit --yes --minimal-output
```

---

## Phase 6: Multiple Cursors & Block Selection

### Task 23: Multiple Cursors

This is the most complex feature. It requires:
1. A `MultiCursor` type that holds multiple Selection ranges
2. Intercepting all text input to apply at each cursor position
3. Rendering multiple cursors and selections
4. Ctrl+D to add next occurrence

**Files:**
- Create: `editor/multicursor.go`
- Create: `editor/multicursor_test.go`
- Modify: `app.go` (intercept input, render cursors)

**Step 1: Implement MultiCursor**

```go
package editor

import "sort"

// Cursor represents a single cursor with optional selection.
type Cursor struct {
	Offset int // cursor position (rune offset)
	Anchor int // selection anchor (same as Offset if no selection)
}

// MultiCursor manages multiple simultaneous cursors.
type MultiCursor struct {
	cursors []Cursor
}

func NewMultiCursor() *MultiCursor {
	return &MultiCursor{cursors: []Cursor{{Offset: 0, Anchor: 0}}}
}

// AddCursor adds a cursor at the given position.
func (mc *MultiCursor) AddCursor(offset int) {
	mc.cursors = append(mc.cursors, Cursor{Offset: offset, Anchor: offset})
	mc.sort()
}

// AddNextOccurrence finds the next occurrence of the currently selected text
// and adds a cursor+selection there.
func (mc *MultiCursor) AddNextOccurrence(text string) {
	if len(mc.cursors) == 0 {
		return
	}
	// Get the selected text from the last cursor
	last := mc.cursors[len(mc.cursors)-1]
	if last.Offset == last.Anchor {
		return // no selection
	}
	start, end := last.Anchor, last.Offset
	if start > end {
		start, end = end, start
	}
	runes := []rune(text)
	selected := string(runes[start:end])

	// Search for next occurrence after the last cursor
	searchFrom := end
	remaining := string(runes[searchFrom:])
	idx := strings.Index(remaining, selected)
	if idx < 0 {
		// Wrap around
		idx = strings.Index(string(runes[:start]), selected)
		if idx < 0 {
			return
		}
		searchFrom = 0
	}
	newStart := searchFrom + len([]rune(remaining[:idx]))
	newEnd := newStart + (end - start)
	mc.cursors = append(mc.cursors, Cursor{Offset: newEnd, Anchor: newStart})
	mc.sort()
}

func (mc *MultiCursor) sort() {
	sort.Slice(mc.cursors, func(i, j int) bool {
		return mc.cursors[i].Offset < mc.cursors[j].Offset
	})
}

// InsertAtAll inserts text at every cursor position, adjusting offsets.
func (mc *MultiCursor) InsertAtAll(text string, insert string) string {
	runes := []rune(text)
	insertRunes := []rune(insert)
	// Apply from end to start to keep offsets valid
	for i := len(mc.cursors) - 1; i >= 0; i-- {
		c := mc.cursors[i]
		offset := c.Offset
		if c.Anchor != c.Offset {
			// Replace selection
			start, end := c.Anchor, c.Offset
			if start > end { start, end = end, start }
			runes = append(runes[:start], append(insertRunes, runes[end:]...)...)
			mc.cursors[i].Offset = start + len(insertRunes)
			mc.cursors[i].Anchor = mc.cursors[i].Offset
		} else {
			runes = append(runes[:offset], append(insertRunes, runes[offset:]...)...)
			mc.cursors[i].Offset = offset + len(insertRunes)
			mc.cursors[i].Anchor = mc.cursors[i].Offset
		}
	}
	// Adjust all cursor offsets
	mc.recalculate(runes)
	return string(runes)
}

func (mc *MultiCursor) recalculate(runes []rune) {
	// Ensure all cursors are within bounds
	for i := range mc.cursors {
		if mc.cursors[i].Offset > len(runes) {
			mc.cursors[i].Offset = len(runes)
		}
		if mc.cursors[i].Anchor > len(runes) {
			mc.cursors[i].Anchor = len(runes)
		}
	}
}

// Cursors returns all cursor positions.
func (mc *MultiCursor) Cursors() []Cursor {
	return mc.cursors
}

// Reset removes all cursors except the primary.
func (mc *MultiCursor) Reset() {
	if len(mc.cursors) > 0 {
		mc.cursors = mc.cursors[:1]
	}
}

// Count returns the number of active cursors.
func (mc *MultiCursor) Count() int {
	return len(mc.cursors)
}
```

**Step 2: Wire Ctrl+D**

In globalKeys:
```go
case terminal.KeyCtrlD:
	app.addNextCursorOccurrence()
	return runtime.Handled()
```

**Step 3: Render multiple cursors**

Add highlights for each cursor position (different background color).

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/multicursor.go editor/multicursor_test.go app.go
buckley commit --yes --minimal-output
```

### Task 24: Block Selection

**Files:**
- Create: `editor/blockselection.go`
- Create: `editor/blockselection_test.go`
- Modify: `app.go`

**Step 1: Add block selection model**

Track start/end rows and column span with explicit active/inactive state.

```go
type BlockSelection struct {
	StartLine int
	EndLine   int
	StartCol  int
	EndCol    int
	Active    bool
}
```

**Step 2: Add keyboard control**

- Alt+Shift+Up: expand up
- Alt+Shift+Down: expand down
- Alt+Shift+Left: expand left
- Alt+Shift+Right: expand right
- Esc: clear selection

**Step 3: Add block edit semantics**

When active, insert/delete operations must apply across all selected lines by column span.

**Step 4: Build and commit**

```bash
cd /home/draco/work/mane && go build -o mane .
git add editor/blockselection.go editor/blockselection_test.go app.go
buckley commit --yes --minimal-output
```

---

## Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| 1 | 1-6 | Fix gaps + core editing (replace, line ops, indent, brackets, folding, wrap) |
| 2 | 7-10 | Navigation & UI (go-to-line, file finder, breadcrumbs, status bar) |
| 3 | 11-17 | LSP client (core, sync, config, completion, diagnostics, definition, rename) |
| 4 | 18-20 | Web mode (FluffyUI backend + custom frontend) |
| 5 | 21-22 | MCP extensions (editor tools + code intelligence resources) |
| 6 | 23-24 | Multiple cursors & block selection |

Total: 24 tasks, incremental commits after each.
