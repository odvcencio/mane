package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/odvcencio/fluffyui/fluffy"
	"github.com/odvcencio/fluffyui/state"
	"github.com/odvcencio/fluffyui/style"
	"github.com/odvcencio/fluffyui/widgets"

	"github.com/odvcencio/mane/commands"
	"github.com/odvcencio/mane/editor"
	"github.com/odvcencio/mane/gotreesitter"
	"github.com/odvcencio/mane/grammars"
)

//go:embed themes/*.fss
var themeFS embed.FS

// loadTheme reads and parses a named FSS theme from the embedded themes directory.
func loadTheme(name string) *style.Stylesheet {
	data, err := themeFS.ReadFile("themes/" + name + ".fss")
	if err != nil {
		return nil // fall back to default
	}
	sheet, err := style.Parse(string(data))
	if err != nil {
		return nil
	}
	return sheet
}

// highlightState holds the syntax highlighting state for the active buffer.
// It manages the highlighter, parse tree, and debounced re-highlighting.
type highlightState struct {
	mu          sync.Mutex
	highlighter *gotreesitter.Highlighter
	tree        *gotreesitter.Tree
	ranges      []gotreesitter.HighlightRange
	lang        *gotreesitter.Language
	timer       *time.Timer
	debounceMs  int
}

func newHighlightState() *highlightState {
	return &highlightState{debounceMs: 50}
}

// setup initializes the highlighter for a given file extension.
// Returns true if a language was found and highlighting is available.
func (hs *highlightState) setup(filename string) bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		hs.highlighter = nil
		hs.tree = nil
		hs.ranges = nil
		hs.lang = nil
		return false
	}

	lang := entry.Language()
	support := grammars.EvaluateParseSupport(*entry, lang)
	if support.Backend == grammars.ParseBackendUnsupported {
		hs.highlighter = nil
		hs.tree = nil
		hs.ranges = nil
		hs.lang = lang
		return false
	}
	hs.lang = lang

	var opts []gotreesitter.HighlighterOption
	if entry.TokenSourceFactory != nil {
		factory := entry.TokenSourceFactory
		opts = append(opts, gotreesitter.WithTokenSourceFactory(func(src []byte) gotreesitter.TokenSource {
			return factory(src, lang)
		}))
	}

	h, err := gotreesitter.NewHighlighter(lang, entry.HighlightQuery, opts...)
	if err != nil {
		hs.highlighter = nil
		return false
	}
	hs.highlighter = h
	hs.tree = nil
	hs.ranges = nil
	return true
}

// highlight runs a full highlight pass on the given source.
func (hs *highlightState) highlight(source []byte) []gotreesitter.HighlightRange {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.highlighter == nil {
		return nil
	}

	if hs.tree != nil {
		hs.ranges, hs.tree = hs.highlighter.HighlightIncremental(source, hs.tree)
	} else {
		hs.ranges, hs.tree = hs.highlighter.HighlightIncremental(source, nil)
	}
	return hs.ranges
}

// scheduleHighlight debounces highlight requests. The callback is invoked
// after debounceMs of inactivity with the latest source.
func (hs *highlightState) scheduleHighlight(source []byte, callback func([]gotreesitter.HighlightRange)) {
	hs.mu.Lock()
	if hs.timer != nil {
		hs.timer.Stop()
	}
	hs.timer = time.AfterFunc(time.Duration(hs.debounceMs)*time.Millisecond, func() {
		ranges := hs.highlight(source)
		callback(ranges)
	})
	hs.mu.Unlock()
}

// Ranges returns the current highlight ranges.
func (hs *highlightState) Ranges() []gotreesitter.HighlightRange {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.ranges
}

// maneApp holds the core state for the editor application.
type maneApp struct {
	tabs      *editor.TabManager
	textArea  *widgets.TextArea
	fileTree  *widgets.DirectoryTree
	status    *state.Signal[string]
	palette   *widgets.CommandPalette
	cancel    context.CancelFunc // for quit command
	highlight *highlightState
	theme     *style.Stylesheet
}

// newManeApp creates a maneApp with the given root directory for the file tree.
func newManeApp(treeRoot string) *maneApp {
	app := &maneApp{
		tabs:      editor.NewTabManager(),
		textArea:  widgets.NewTextArea(),
		status:    state.NewSignal[string](" untitled"),
		highlight: newHighlightState(),
	}

	app.textArea.SetLabel("Editor")
	app.textArea.SetShowLineNumbers(true)
	app.textArea.SetTabMode(true) // literal tabs for code editing

	app.fileTree = widgets.NewDirectoryTree(treeRoot,
		widgets.WithLazyLoad(true),
		widgets.WithOnSelect(func(path string) {
			app.openFile(path)
		}),
	)

	app.textArea.SetOnChange(func(text string) {
		buf := app.tabs.ActiveBuffer()
		if buf == nil {
			return
		}
		buf.SetText(text)
		app.updateStatus()

		// Debounced re-highlight on text change.
		app.highlight.scheduleHighlight([]byte(text), func(ranges []gotreesitter.HighlightRange) {
			app.applyHighlights(text, ranges)
		})
	})

	return app
}

// byteOffsetToRuneOffset builds a byte-to-rune offset lookup for the given text.
func byteOffsetToRuneOffset(text string) []int {
	// Map byte offsets to rune offsets.
	mapping := make([]int, len(text)+1)
	runeIdx := 0
	for byteIdx := 0; byteIdx < len(text); {
		mapping[byteIdx] = runeIdx
		_, size := utf8.DecodeRuneInString(text[byteIdx:])
		for j := 1; j < size && byteIdx+j <= len(text); j++ {
			mapping[byteIdx+j] = runeIdx
		}
		byteIdx += size
		runeIdx++
	}
	mapping[len(text)] = runeIdx
	return mapping
}

// applyHighlights converts gotreesitter.HighlightRange values to TextAreaHighlight
// and sets them on the TextArea.
func (a *maneApp) applyHighlights(text string, ranges []gotreesitter.HighlightRange) {
	if a.theme == nil || len(ranges) == 0 {
		a.textArea.ClearHighlights()
		return
	}

	mapping := byteOffsetToRuneOffset(text)

	highlights := make([]widgets.TextAreaHighlight, 0, len(ranges))
	for _, r := range ranges {
		startByte := int(r.StartByte)
		endByte := int(r.EndByte)
		if startByte > len(text) {
			startByte = len(text)
		}
		if endByte > len(text) {
			endByte = len(text)
		}

		resolved := a.theme.ResolveClass(r.Capture)
		if resolved.IsZero() {
			continue
		}
		bs := resolved.ToBackend()

		highlights = append(highlights, widgets.TextAreaHighlight{
			Start: mapping[startByte],
			End:   mapping[endByte],
			Style: bs,
		})
	}
	a.textArea.SetHighlights(highlights)
}

// openFile opens a file by path through the TabManager and loads its content
// into the TextArea.
func (a *maneApp) openFile(path string) {
	_, err := a.tabs.OpenFile(path)
	if err != nil {
		a.status.Set(fmt.Sprintf(" error: %v", err))
		return
	}

	// Set up syntax highlighting for the file's language.
	a.highlight.setup(filepath.Base(path))

	a.syncTextArea()
	a.updateStatus()

	// Run initial highlight and apply to TextArea.
	buf := a.tabs.ActiveBuffer()
	if buf != nil {
		text := buf.Text()
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
	}
}

// syncTextArea loads the active buffer's text into the TextArea widget.
func (a *maneApp) syncTextArea() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		a.textArea.SetText("")
		return
	}
	a.textArea.SetText(buf.Text())
}

// updateStatus refreshes the status bar signal with the current buffer info.
// Format: " {title}{dirty}  {lang}  Ln {row+1}, Col {col+1}"
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

	// Show detected language.
	langName := ""
	if entry := grammars.DetectLanguage(filepath.Base(buf.Path())); entry != nil {
		langName = "  " + strings.ToUpper(entry.Name)
	}

	col, row := a.textArea.CursorPosition()
	a.status.Set(fmt.Sprintf(" %s%s%s  Ln %d, Col %d", buf.Title(), dirty, langName, row+1, col+1))
}

// cmdSaveFile saves the active buffer to disk.
func (a *maneApp) cmdSaveFile() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	if buf.Untitled() {
		a.status.Set("Cannot save untitled file")
		return
	}
	buf.SetText(a.textArea.Text())
	if err := buf.Save(); err != nil {
		a.status.Set(fmt.Sprintf("Save error: %v", err))
		return
	}
	a.status.Set(fmt.Sprintf("Saved %s", buf.Title()))
	a.updateStatus()
}

// cmdNewFile creates a new untitled buffer and switches to it.
func (a *maneApp) cmdNewFile() {
	a.tabs.NewUntitled()
	a.textArea.SetText("")
	a.highlight.setup("") // no language for untitled
	a.updateStatus()
}

// cmdCloseTab closes the active tab and switches to the next buffer.
func (a *maneApp) cmdCloseTab() {
	if a.tabs.Count() == 0 {
		return
	}
	a.tabs.Close(a.tabs.Active())
	buf := a.tabs.ActiveBuffer()
	if buf != nil {
		text := buf.Text()
		a.textArea.SetText(text)
		a.highlight.setup(filepath.Base(buf.Path()))
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
	} else {
		a.textArea.SetText("")
		a.highlight.setup("")
		a.textArea.ClearHighlights()
	}
	a.updateStatus()
}

// cmdUndo undoes the last edit via the TextArea's built-in undo.
func (a *maneApp) cmdUndo() {
	a.textArea.Undo()
}

// cmdRedo redoes the last undone edit via the TextArea's built-in redo.
func (a *maneApp) cmdRedo() {
	a.textArea.Redo()
}

// run constructs the editor layout and starts the FluffyUI app.
func run(ctx context.Context, root, theme string, opts ...fluffy.AppOption) error {
	sheet := loadTheme(theme)
	if sheet != nil {
		opts = append(opts, fluffy.WithStylesheet(sheet))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	// Determine whether root is a directory or a file.
	// If a file, use its parent directory for the tree and open that file.
	var filesToOpen []string
	treeRoot := absRoot

	info, err := os.Stat(absRoot)
	if err == nil && !info.IsDir() {
		// Root is a file: use its parent as the tree root.
		treeRoot = filepath.Dir(absRoot)
		filesToOpen = append(filesToOpen, absRoot)
	}

	app := newManeApp(treeRoot)
	app.cancel = cancel
	app.theme = sheet
	if sheet != nil {
		// Set gutter style from theme's .comment class (dimmed text).
		gutterResolved := sheet.ResolveClass("comment")
		if !gutterResolved.IsZero() {
			app.textArea.SetGutterStyle(gutterResolved.ToBackend())
		}
	}

	// Build the command palette with editor actions.
	app.palette = widgets.NewCommandPalette(commands.AllCommands(commands.Actions{
		SaveFile:      app.cmdSaveFile,
		NewFile:       app.cmdNewFile,
		CloseTab:      app.cmdCloseTab,
		ToggleSidebar: func() {}, // placeholder
		Quit:          func() { cancel() },
		Undo:          app.cmdUndo,
		Redo:          app.cmdRedo,
	})...)

	// Open files from CLI args, or create an untitled buffer if none.
	for _, f := range filesToOpen {
		app.openFile(f)
	}
	if app.tabs.Count() == 0 {
		app.tabs.NewUntitled()
		app.syncTextArea()
		app.updateStatus()
	}

	// Status bar: reactive label driven by the status signal.
	statusBar := fluffy.ReactiveText(func() string {
		return app.status.Get()
	}, app.status)

	// Horizontal split: file tree (22%) | editor (78%).
	splitter := widgets.NewSplitter(app.fileTree, app.textArea)
	splitter.Ratio = 0.22

	// Vertical layout: splitter fills space, status bar fixed at bottom.
	layout := fluffy.VFlex(
		fluffy.Expanded(splitter),
		fluffy.Fixed(statusBar),
	)

	// Stack the palette on top of the layout for overlay rendering.
	rootWidget := widgets.NewStack(layout, app.palette)

	return fluffy.RunContext(ctx, rootWidget, opts...)
}
