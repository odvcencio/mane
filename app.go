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

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/fluffy"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/state"
	"github.com/odvcencio/fluffyui/style"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/mane/commands"
	"github.com/odvcencio/mane/editor"
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

// contentSlot is a simple wrapper widget whose child can be swapped at runtime.
type contentSlot struct {
	widgets.Base
	child runtime.Widget
}

func (c *contentSlot) setChild(w runtime.Widget) {
	c.child = w
}

func (c *contentSlot) Measure(constraints runtime.Constraints) runtime.Size {
	if c.child != nil {
		return c.child.Measure(constraints)
	}
	return runtime.Size{}
}

func (c *contentSlot) Layout(bounds runtime.Rect) {
	c.Base.Layout(bounds)
	if c.child != nil {
		c.child.Layout(bounds)
	}
}

func (c *contentSlot) Render(ctx runtime.RenderContext) {
	if c.child != nil {
		c.child.Render(ctx)
	}
}

func (c *contentSlot) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if c.child != nil {
		return c.child.HandleMessage(msg)
	}
	return runtime.Unhandled()
}

func (c *contentSlot) ChildWidgets() []runtime.Widget {
	if c.child != nil {
		return []runtime.Widget{c.child}
	}
	return nil
}

// globalKeys is an invisible widget that intercepts global key shortcuts.
type globalKeys struct {
	widgets.Base
	onKey func(key runtime.KeyMsg) runtime.HandleResult
}

func (g *globalKeys) Measure(runtime.Constraints) runtime.Size { return runtime.Size{} }
func (g *globalKeys) Render(runtime.RenderContext)             {}

func (g *globalKeys) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if key, ok := msg.(runtime.KeyMsg); ok && g.onKey != nil {
		return g.onKey(key)
	}
	return runtime.Unhandled()
}

// maneApp holds the core state for the editor application.
type maneApp struct {
	tabs      *editor.TabManager
	textArea  *widgets.TextArea
	fileTree  *widgets.DirectoryTree
	tabBar    *tabBar
	status    *state.Signal[string]
	palette   *widgets.CommandPalette
	search    *widgets.SearchWidget
	replaceW  *replaceWidget
	cancel    context.CancelFunc // for quit command
	highlight *highlightState
	theme     *style.Stylesheet

	// Sidebar toggle
	sidebarVisible bool
	splitter       *widgets.Splitter
	slot           *contentSlot

	// Search state
	searchMatches    []editor.Range
	searchCurrent    int
	syntaxHighlights []widgets.TextAreaHighlight // cached syntax highlights
}

// newManeApp creates a maneApp with the given root directory for the file tree.
func newManeApp(treeRoot string) *maneApp {
	app := &maneApp{
		tabs:           editor.NewTabManager(),
		textArea:       widgets.NewTextArea(),
		status:         state.NewSignal[string](" untitled"),
		highlight:      newHighlightState(),
		sidebarVisible: true,
	}

	app.tabBar = newTabBar()
	app.tabBar.onClick = func(index int) {
		app.switchTab(index)
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
		a.syntaxHighlights = nil
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

	// Cache syntax highlights for merging with search highlights
	a.syntaxHighlights = highlights
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
	a.syncTabBar()
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

// toggleSidebar shows or hides the file tree sidebar.
func (a *maneApp) toggleSidebar() {
	a.sidebarVisible = !a.sidebarVisible
	if a.sidebarVisible {
		a.slot.setChild(a.splitter)
	} else {
		a.slot.setChild(a.textArea)
	}
}

// cmdFind opens the search widget as an overlay.
func (a *maneApp) cmdFind() runtime.HandleResult {
	a.search.Focus()
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.search})
}

// onSearch handles search query changes from the SearchWidget.
func (a *maneApp) onSearch(query string) {
	if query == "" {
		a.searchMatches = nil
		a.searchCurrent = 0
		a.search.SetMatchInfo(0, 0)
		// Restore syntax-only highlights
		a.textArea.SetHighlights(a.syntaxHighlights)
		return
	}

	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	// Find matches (byte ranges)
	a.searchMatches = buf.Find(query)
	a.searchCurrent = 0

	if len(a.searchMatches) > 0 {
		a.search.SetMatchInfo(0, len(a.searchMatches))
		// Jump to first match
		a.jumpToMatch(0)
	} else {
		a.search.SetMatchInfo(0, 0)
	}

	a.applySearchHighlights()
}

// onSearchNext moves to the next search match.
func (a *maneApp) onSearchNext() {
	if len(a.searchMatches) == 0 {
		return
	}
	a.searchCurrent = (a.searchCurrent + 1) % len(a.searchMatches)
	a.search.SetMatchInfo(a.searchCurrent, len(a.searchMatches))
	a.jumpToMatch(a.searchCurrent)
	a.applySearchHighlights()
}

// onSearchPrev moves to the previous search match.
func (a *maneApp) onSearchPrev() {
	if len(a.searchMatches) == 0 {
		return
	}
	a.searchCurrent--
	if a.searchCurrent < 0 {
		a.searchCurrent = len(a.searchMatches) - 1
	}
	a.search.SetMatchInfo(a.searchCurrent, len(a.searchMatches))
	a.jumpToMatch(a.searchCurrent)
	a.applySearchHighlights()
}

// onSearchClose clears search state when the search widget is dismissed.
func (a *maneApp) onSearchClose() {
	a.searchMatches = nil
	a.searchCurrent = 0
	// Restore syntax-only highlights
	a.textArea.SetHighlights(a.syntaxHighlights)
}

// cmdReplace opens the replace widget as an overlay.
func (a *maneApp) cmdReplace() runtime.HandleResult {
	a.replaceW.Focus()
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.replaceW})
}

// onReplaceSearch handles search query changes from the replace widget.
// It reuses the same search state as cmdFind so highlights stay consistent.
func (a *maneApp) onReplaceSearch(query string) {
	if query == "" {
		a.searchMatches = nil
		a.searchCurrent = 0
		a.replaceW.SetMatchInfo(0, 0)
		a.textArea.SetHighlights(a.syntaxHighlights)
		return
	}

	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	a.searchMatches = buf.Find(query)
	a.searchCurrent = 0

	if len(a.searchMatches) > 0 {
		a.replaceW.SetMatchInfo(0, len(a.searchMatches))
		a.jumpToMatch(0)
	} else {
		a.replaceW.SetMatchInfo(0, 0)
	}

	a.applySearchHighlights()
}

// onReplaceNext moves to the next search match (from replace widget).
func (a *maneApp) onReplaceNext() {
	if len(a.searchMatches) == 0 {
		return
	}
	a.searchCurrent = (a.searchCurrent + 1) % len(a.searchMatches)
	a.replaceW.SetMatchInfo(a.searchCurrent, len(a.searchMatches))
	a.jumpToMatch(a.searchCurrent)
	a.applySearchHighlights()
}

// onReplacePrev moves to the previous search match (from replace widget).
func (a *maneApp) onReplacePrev() {
	if len(a.searchMatches) == 0 {
		return
	}
	a.searchCurrent--
	if a.searchCurrent < 0 {
		a.searchCurrent = len(a.searchMatches) - 1
	}
	a.replaceW.SetMatchInfo(a.searchCurrent, len(a.searchMatches))
	a.jumpToMatch(a.searchCurrent)
	a.applySearchHighlights()
}

// onReplace replaces the current search match and advances to the next one.
func (a *maneApp) onReplace(search, replace string) {
	if search == "" || len(a.searchMatches) == 0 {
		return
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	// Replace the current match.
	if a.searchCurrent >= 0 && a.searchCurrent < len(a.searchMatches) {
		r := a.searchMatches[a.searchCurrent]
		buf.Replace(search, replace, editor.Range{Start: r.Start, End: r.End})
	}

	// Re-sync the text area from the buffer.
	a.syncTextArea()
	a.updateStatus()

	// Re-run search to refresh matches with updated text.
	a.searchMatches = buf.Find(search)
	if len(a.searchMatches) == 0 {
		a.searchCurrent = 0
		a.replaceW.SetMatchInfo(0, 0)
		a.textArea.SetHighlights(a.syntaxHighlights)
		return
	}

	// Keep cursor at same index, clamped to new match count.
	if a.searchCurrent >= len(a.searchMatches) {
		a.searchCurrent = 0
	}
	a.replaceW.SetMatchInfo(a.searchCurrent, len(a.searchMatches))
	a.jumpToMatch(a.searchCurrent)
	a.applySearchHighlights()

	// Re-highlight syntax.
	text := buf.Text()
	a.highlight.scheduleHighlight([]byte(text), func(ranges []gotreesitter.HighlightRange) {
		a.applyHighlights(text, ranges)
	})
}

// onReplaceAll replaces all occurrences and updates the UI.
func (a *maneApp) onReplaceAll(search, replace string) {
	if search == "" {
		return
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	count := buf.ReplaceAll(search, replace)

	// Re-sync the text area from the buffer.
	a.syncTextArea()
	a.updateStatus()

	// Clear search state since all matches are gone.
	a.searchMatches = nil
	a.searchCurrent = 0
	a.replaceW.SetMatchInfo(0, 0)
	a.textArea.SetHighlights(a.syntaxHighlights)

	a.status.Set(fmt.Sprintf(" Replaced %d occurrence(s)", count))

	// Re-highlight syntax.
	text := buf.Text()
	a.highlight.scheduleHighlight([]byte(text), func(ranges []gotreesitter.HighlightRange) {
		a.applyHighlights(text, ranges)
	})
}

// onReplaceClose clears search state when the replace widget is dismissed.
func (a *maneApp) onReplaceClose() {
	a.searchMatches = nil
	a.searchCurrent = 0
	a.textArea.SetHighlights(a.syntaxHighlights)
}

// jumpToMatch moves the cursor to the given match index.
func (a *maneApp) jumpToMatch(idx int) {
	if idx < 0 || idx >= len(a.searchMatches) {
		return
	}
	m := a.searchMatches[idx]
	text := a.textArea.Text()
	mapping := byteOffsetToRuneOffset(text)
	runeStart := mapping[m.Start]
	a.textArea.SetCursorOffset(runeStart)
}

// applySearchHighlights merges syntax highlights with search match highlights.
func (a *maneApp) applySearchHighlights() {
	text := a.textArea.Text()
	mapping := byteOffsetToRuneOffset(text)

	// Start with syntax highlights
	merged := make([]widgets.TextAreaHighlight, len(a.syntaxHighlights))
	copy(merged, a.syntaxHighlights)

	// Match styles
	matchStyle := backend.DefaultStyle().Background(backend.ColorYellow).Foreground(backend.ColorBlack)
	currentStyle := backend.DefaultStyle().Background(backend.ColorRGB(0xFF, 0x88, 0x00)).Foreground(backend.ColorBlack)

	for i, m := range a.searchMatches {
		start := m.Start
		end := m.End
		if start > len(text) {
			start = len(text)
		}
		if end > len(text) {
			end = len(text)
		}
		s := matchStyle
		if i == a.searchCurrent {
			s = currentStyle
		}
		merged = append(merged, widgets.TextAreaHighlight{
			Start: mapping[start],
			End:   mapping[end],
			Style: s,
		})
	}

	a.textArea.SetHighlights(merged)
}

// syncTabBar rebuilds the tab bar from the current TabManager state.
func (a *maneApp) syncTabBar() {
	buffers := a.tabs.Buffers()
	tabs := make([]tabInfo, len(buffers))
	for i, buf := range buffers {
		tabs[i] = tabInfo{
			title: buf.Title(),
			dirty: buf.Dirty(),
		}
	}
	a.tabBar.setTabs(tabs, a.tabs.Active())
}

// switchTab switches to the tab at the given index and reloads the TextArea.
func (a *maneApp) switchTab(index int) {
	a.tabs.SetActive(index)
	buf := a.tabs.ActiveBuffer()
	if buf != nil {
		text := buf.Text()
		a.textArea.SetText(text)
		a.highlight.setup(filepath.Base(buf.Path()))
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
	}
	a.syncTabBar()
	a.updateStatus()
}

// prevTab switches to the previous tab (wrapping around).
func (a *maneApp) prevTab() {
	if a.tabs.Count() <= 1 {
		return
	}
	idx := a.tabs.Active() - 1
	if idx < 0 {
		idx = a.tabs.Count() - 1
	}
	a.switchTab(idx)
}

// nextTab switches to the next tab (wrapping around).
func (a *maneApp) nextTab() {
	if a.tabs.Count() <= 1 {
		return
	}
	idx := (a.tabs.Active() + 1) % a.tabs.Count()
	a.switchTab(idx)
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
	a.syncTabBar()
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
	a.syncTabBar()
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
func run(ctx context.Context, paths []string, theme string, opts ...fluffy.AppOption) error {
	sheet := loadTheme(theme)
	if sheet != nil {
		opts = append(opts, fluffy.WithStylesheet(sheet))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Classify args into directories and files. Use the first directory
	// (or the parent of the first file, or cwd) as the tree root.
	var filesToOpen []string
	treeRoot := ""

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if treeRoot == "" {
				treeRoot = abs
			}
		} else {
			filesToOpen = append(filesToOpen, abs)
			if treeRoot == "" {
				treeRoot = filepath.Dir(abs)
			}
		}
	}

	if treeRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			treeRoot = "."
		} else {
			treeRoot = cwd
		}
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

	// Set up search widget
	app.search = widgets.NewSearchWidget()
	app.search.SetOnSearch(app.onSearch)
	app.search.SetOnNavigate(app.onSearchNext, app.onSearchPrev)
	app.search.SetOnClose(app.onSearchClose)

	// Set up replace widget
	app.replaceW = newReplaceWidget()
	app.replaceW.onSearch = app.onReplaceSearch
	app.replaceW.onNext = app.onReplaceNext
	app.replaceW.onPrev = app.onReplacePrev
	app.replaceW.onReplace = app.onReplace
	app.replaceW.onReplaceAll = app.onReplaceAll
	app.replaceW.onClose = app.onReplaceClose

	// Build the command palette with editor actions.
	app.palette = widgets.NewCommandPalette(commands.AllCommands(commands.Actions{
		SaveFile:      app.cmdSaveFile,
		NewFile:       app.cmdNewFile,
		CloseTab:      app.cmdCloseTab,
		ToggleSidebar: app.toggleSidebar,
		Quit:          func() { cancel() },
		Undo:          app.cmdUndo,
		Redo:          app.cmdRedo,
		Find:          func() { app.cmdFind() },
		Replace:       func() { app.cmdReplace() },
	})...)

	// Open files from CLI args, or create an untitled buffer if none.
	for _, f := range filesToOpen {
		app.openFile(f)
	}
	if app.tabs.Count() == 0 {
		app.tabs.NewUntitled()
		app.syncTextArea()
		app.syncTabBar()
		app.updateStatus()
	}

	// Status bar: reactive label driven by the status signal.
	statusBar := fluffy.ReactiveText(func() string {
		return app.status.Get()
	}, app.status)

	// Horizontal split: file tree (22%) | editor (78%).
	app.splitter = widgets.NewSplitter(app.fileTree, app.textArea)
	app.splitter.Ratio = 0.22

	// Content slot: swappable between splitter (sidebar visible) and textArea only.
	app.slot = &contentSlot{child: app.splitter}

	// Vertical layout: tab bar, content fills space, status bar fixed at bottom.
	layout := fluffy.VFlex(
		fluffy.Fixed(app.tabBar),
		fluffy.Expanded(app.slot),
		fluffy.Fixed(statusBar),
	)

	// Global key interceptor for shortcuts that need to work regardless of focus.
	keys := &globalKeys{onKey: func(key runtime.KeyMsg) runtime.HandleResult {
		switch key.Key {
		case terminal.KeyCtrlP:
			app.palette.Toggle()
			return runtime.Handled()
		case terminal.KeyCtrlF:
			return app.cmdFind()
		case terminal.KeyRune:
			if key.Ctrl && key.Rune == 'h' {
				return app.cmdReplace()
			}
		case terminal.KeyCtrlB:
			app.toggleSidebar()
			return runtime.Handled()
		case terminal.KeyCtrlS:
			app.cmdSaveFile()
			return runtime.Handled()
		case terminal.KeyCtrlN:
			app.cmdNewFile()
			return runtime.Handled()
		case terminal.KeyCtrlW:
			app.cmdCloseTab()
			return runtime.Handled()
		case terminal.KeyCtrlQ:
			cancel()
			return runtime.Handled()
		case terminal.KeyPageUp:
			if key.Ctrl {
				app.prevTab()
				return runtime.Handled()
			}
		case terminal.KeyPageDown:
			if key.Ctrl {
				app.nextTab()
				return runtime.Handled()
			}
		}
		return runtime.Unhandled()
	}}

	// Stack: layout at bottom, palette in middle, global keys on top (gets events first).
	rootWidget := widgets.NewStack(layout, app.palette, keys)

	return fluffy.RunContext(ctx, rootWidget, opts...)
}
