package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/clipboard"
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
	"github.com/odvcencio/mane/lsp"
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

// detectFoldRegions returns fold regions from the current parse tree when
// available, falling back to brace heuristics for unsupported languages.
func (hs *highlightState) detectFoldRegions(source string) []editor.FoldRegion {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.tree != nil && hs.lang != nil {
		regions := foldRegionsFromTree(hs.tree.RootNode(), hs.lang)
		if len(regions) > 0 {
			return regions
		}
	}
	return editor.DetectFoldRegions(source)
}

// foldRegionsFromTree extracts fold regions from named multiline nodes.
func foldRegionsFromTree(root *gotreesitter.Node, lang *gotreesitter.Language) []editor.FoldRegion {
	if root == nil || lang == nil {
		return nil
	}

	seen := make(map[[2]int]struct{})
	regions := make([]editor.FoldRegion, 0, 64)
	var walk func(node *gotreesitter.Node, isRoot bool)

	walk = func(node *gotreesitter.Node, isRoot bool) {
		if node == nil {
			return
		}

		if !isRoot && node.IsNamed() {
			start := int(node.StartPoint().Row)
			end := int(node.EndPoint().Row)
			if end > start && shouldFoldNode(node, lang) {
				key := [2]int{start, end}
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					regions = append(regions, editor.FoldRegion{
						StartLine: start,
						EndLine:   end,
					})
				}
			}
		}

		n := node.NamedChildCount()
		for i := 0; i < n; i++ {
			walk(node.NamedChild(i), false)
		}
	}

	walk(root, true)
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].StartLine == regions[j].StartLine {
			return regions[i].EndLine < regions[j].EndLine
		}
		return regions[i].StartLine < regions[j].StartLine
	})
	return regions
}

func shouldFoldNode(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	if node == nil || node.NamedChildCount() == 0 {
		return false
	}

	nodeType := strings.ToLower(node.Type(lang))
	switch nodeType {
	case "source_file", "program", "translation_unit", "module":
		return false
	}

	// Common structural node kinds across languages.
	keywords := []string{
		"block", "body", "declaration", "statement", "function", "method",
		"class", "struct", "interface", "enum", "object", "array", "map",
		"list", "switch", "case", "if", "else", "for", "while", "loop",
		"try", "catch", "finally", "impl", "namespace", "comment",
	}
	for _, kw := range keywords {
		if strings.Contains(nodeType, kw) {
			return true
		}
	}

	// Generic fallback for unnamed grammar variants: multiline nodes with
	// multiple named children are usually foldable containers.
	return node.NamedChildCount() >= 2
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
	onKey   func(key runtime.KeyMsg) runtime.HandleResult
	onMouse func(mouse runtime.MouseMsg) runtime.HandleResult
	onPaste func(paste runtime.PasteMsg) runtime.HandleResult
}

func (g *globalKeys) Measure(runtime.Constraints) runtime.Size { return runtime.Size{} }
func (g *globalKeys) Render(runtime.RenderContext)             {}

func (g *globalKeys) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if mouse, ok := msg.(runtime.MouseMsg); ok && g.onMouse != nil {
		if result := g.onMouse(mouse); result.Handled {
			return result
		}
	}
	if paste, ok := msg.(runtime.PasteMsg); ok && g.onPaste != nil {
		if result := g.onPaste(paste); result.Handled {
			return result
		}
	}
	if key, ok := msg.(runtime.KeyMsg); ok && g.onKey != nil {
		return g.onKey(key)
	}
	return runtime.Unhandled()
}

// maneApp holds the core state for the editor application.
type maneApp struct {
	tabs        *editor.TabManager
	textArea    *widgets.TextArea
	fileTree    *widgets.DirectoryTree
	treeRoot    string
	tabBar      *tabBar
	breadcrumbs *widgets.Breadcrumb
	status      *state.Signal[string]
	palette     *widgets.CommandPalette
	search      *widgets.SearchWidget
	replaceW    *replaceWidget
	gotoLineW   *gotoLineWidget
	fileFinder  *fileFinderWidget
	cancel      context.CancelFunc // for quit command
	highlight   *highlightState
	theme       *style.Stylesheet

	// Sidebar toggle
	sidebarVisible bool
	splitter       *widgets.Splitter
	slot           *contentSlot

	// Search state
	searchMatches     []editor.Range
	searchCurrent     int
	syntaxHighlights  []widgets.TextAreaHighlight // cached syntax highlights
	bracketHighlights []widgets.TextAreaHighlight // bracket match highlights
	multiCursor       *editor.MultiCursor
	multiHighlights   []widgets.TextAreaHighlight // cached multi-cursor highlights
	blockHighlights   []widgets.TextAreaHighlight // cached block selection highlights

	// File finder cache.
	finderRoot string
	finderPath []finderFile

	// Git branch cache.
	gitBranchPath  string
	gitBranchCache string

	// Auto-indent state
	suppressChange bool

	// View state.
	wordWrap       bool
	foldState      *editor.FoldState
	blockSelection *editor.BlockSelection
	blockAnchorRow int
	blockAnchorCol int
	blockFocusRow  int
	blockFocusCol  int

	// LSP integration.
	lspClients     map[string]*lsp.Client
	lspDocVersions map[string]int
	lspServers     map[string]lsp.ServerConfig
	lspDiagnostics map[string][]lsp.Diagnostic
	diagnostics    []widgets.TextAreaHighlight // cached LSP diagnostics highlights
	lspPalette     *widgets.CommandPalette     // reused for completion/references/code-action UI
	renameW        *renameWidget               // rename symbol overlay

	lspCtx    context.Context
	lspCancel context.CancelFunc
	lspMu     sync.Mutex
}

// newManeApp creates a maneApp with the given root directory for the file tree.
func newManeApp(treeRoot string) *maneApp {
	servers := lsp.DefaultServers()
	applyLSPServerOverrides(servers, treeRoot)

	app := &maneApp{
		tabs:           editor.NewTabManager(),
		textArea:       widgets.NewTextArea(),
		status:         state.NewSignal[string](" untitled"),
		highlight:      newHighlightState(),
		multiCursor:    editor.NewMultiCursor(),
		treeRoot:       treeRoot,
		sidebarVisible: true,
		lspClients:     make(map[string]*lsp.Client),
		lspDocVersions: make(map[string]int),
		lspServers:     servers,
		lspDiagnostics: make(map[string][]lsp.Diagnostic),
		wordWrap:       false,
		foldState:      editor.NewFoldState(),
		blockSelection: editor.NewBlockSelection(),
	}

	app.tabBar = newTabBar()
	app.tabBar.onClick = func(index int) {
		app.switchTab(index)
	}

	app.breadcrumbs = widgets.NewBreadcrumb()
	app.breadcrumbs.SetOnNavigate(func(index int) {
		// Keep navigation in sync with breadcrumb click actions.
		if index >= 0 && index < len(app.breadcrumbs.Items) {
			if item := app.breadcrumbs.Items[index]; item.OnClick != nil {
				item.OnClick()
			}
		}
	})

	app.textArea.SetLabel("Editor")
	app.textArea.SetShowLineNumbers(true)
	app.textArea.SetTabMode(true) // literal tabs for code editing
	app.textArea.SetWordWrap(app.wordWrap)

	app.fileTree = widgets.NewDirectoryTree(treeRoot,
		widgets.WithLazyLoad(true),
		widgets.WithOnSelect(func(path string) {
			_ = app.openFile(path)
		}),
	)

	app.textArea.SetOnChange(func(text string) {
		if app.suppressChange {
			return
		}
		buf := app.tabs.ActiveBuffer()
		if buf == nil {
			return
		}
		buf.SetText(text)
		app.updateStatus()

		// Auto-indent: detect if newline was just inserted
		offset := app.textArea.CursorOffset()
		runes := []rune(text)
		if offset > 0 && offset <= len(runes) && runes[offset-1] == '\n' {
			// Find the line above the cursor
			// Convert rune offset to byte offset for string operations
			byteOffset := len(string(runes[:offset-1]))
			lineStart := strings.LastIndex(text[:byteOffset], "\n")
			if lineStart < 0 {
				lineStart = 0
			} else {
				lineStart++
			}
			lineAbove := text[lineStart:byteOffset]
			indent := editor.ComputeIndent(lineAbove)
			if indent != "" {
				// Insert indent after the newline
				newRunes := make([]rune, 0, len(runes)+len([]rune(indent)))
				newRunes = append(newRunes, runes[:offset]...)
				newRunes = append(newRunes, []rune(indent)...)
				newRunes = append(newRunes, runes[offset:]...)
				newText := string(newRunes)
				buf.SetText(newText)
				app.suppressChange = true
				app.textArea.SetText(newText)
				app.textArea.SetCursorOffset(offset + len([]rune(indent)))
				app.suppressChange = false
				text = newText // use new text for highlighting
			}
		}

		// Debounced re-highlight on text change.
		app.syncMultiCursorFromTextArea()
		app.highlight.scheduleHighlight([]byte(text), func(ranges []gotreesitter.HighlightRange) {
			app.applyHighlights(text, ranges)
			app.updateFoldRegions(text)
		})

		app.scheduleLspDidChange(buf, text)
		app.updateBracketMatch()
		app.mergeAllHighlights()
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

func fileURI(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := &url.URL{
		Scheme: "file",
		Path:   uriPath,
	}
	return u.String()
}

func languageIDFromPath(path string) string {
	entry := grammars.DetectLanguage(filepath.Base(path))
	if entry == nil {
		return ""
	}
	return strings.ToLower(entry.Name)
}

func lspConfigSearchPaths(treeRoot string) []string {
	paths := make([]string, 0, 3)
	if envPath := strings.TrimSpace(os.Getenv("MANE_LSP_CONFIG")); envPath != "" {
		paths = append(paths, envPath)
	}
	if treeRoot != "" {
		paths = append(paths, filepath.Join(treeRoot, ".mane-lsp.json"))
	}
	if cfgRoot, err := os.UserConfigDir(); err == nil && cfgRoot != "" {
		paths = append(paths, filepath.Join(cfgRoot, "mane", "lsp.json"))
	}
	return paths
}

func applyLSPServerOverrides(servers map[string]lsp.ServerConfig, treeRoot string) {
	if len(servers) == 0 {
		return
	}

	for _, configPath := range lspConfigSearchPaths(treeRoot) {
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		overrides := make(map[string]lsp.ServerConfig)
		if err := json.Unmarshal(data, &overrides); err != nil {
			continue
		}

		for langID, cfg := range overrides {
			langID = strings.ToLower(strings.TrimSpace(langID))
			cfg.Command = strings.TrimSpace(cfg.Command)
			if langID == "" || cfg.Command == "" {
				continue
			}
			servers[langID] = cfg
		}
		break
	}
}

func filePathFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if u.Scheme != "file" {
		return uri
	}
	path := u.Path
	if path == "" {
		return ""
	}
	path, err = url.PathUnescape(path)
	if err != nil {
		path = u.Path
	}
	if strings.HasPrefix(path, "/") && len(path) >= 3 && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// lspOffsetFromPosition converts an LSP position to a byte offset.
func lspOffsetFromPosition(text string, pos lsp.Position) int {
	lines := strings.Split(text, "\n")
	lineIdx := pos.Line
	if lineIdx < 0 {
		return 0
	}
	if lineIdx >= len(lines) {
		return len(text)
	}
	runeCol := pos.Character
	if runeCol < 0 {
		runeCol = 0
	}
	lineText := []rune(lines[lineIdx])
	if runeCol > len(lineText) {
		runeCol = len(lineText)
	}
	colBytes := len(string(lineText[:runeCol]))

	prefixLen := 0
	for i := 0; i < lineIdx; i++ {
		prefixLen += len(lines[i]) + 1
	}
	offset := prefixLen + colBytes
	if offset > len(text) {
		return len(text)
	}
	return offset
}

// runeOffsetToByteOffset converts a rune offset to a byte offset.
func runeOffsetToByteOffset(text string, runeOffset int) int {
	if runeOffset <= 0 {
		return 0
	}
	total := utf8.RuneCountInString(text)
	if runeOffset >= total {
		return len(text)
	}
	runeIndex := 0
	for byteIndex := range text {
		if runeIndex == runeOffset {
			return byteIndex
		}
		runeIndex++
	}
	return len(text)
}

// lspPositionFromByteOffset converts a byte offset to an LSP position.
func lspPositionFromByteOffset(text string, byteOffset int) lsp.Position {
	if byteOffset <= 0 {
		return lsp.Position{}
	}
	if byteOffset > len(text) {
		byteOffset = len(text)
	}
	prefix := text[:byteOffset]
	line := strings.Count(prefix, "\n")
	lineStart := strings.LastIndex(prefix, "\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	return lsp.Position{
		Line:      line,
		Character: utf8.RuneCountInString(prefix[lineStart:byteOffset]),
	}
}

func lspRangeToByteOffsets(text string, rng lsp.Range) (int, int) {
	start := lspOffsetFromPosition(text, rng.Start)
	end := lspOffsetFromPosition(text, rng.End)
	if end < start {
		end = start
	}
	if start < 0 {
		start = 0
	}
	if start > len(text) {
		start = len(text)
	}
	if end > len(text) {
		end = len(text)
	}
	return start, end
}

func (a *maneApp) activeLSPSession() (*editor.Buffer, string, string, *lsp.Client, error) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return nil, "", "", nil, fmt.Errorf("no active buffer")
	}
	if buf.Path() == "" {
		return nil, "", "", nil, fmt.Errorf("buffer has no file path")
	}
	uri := fileURI(buf.Path())
	if uri == "" {
		return nil, "", "", nil, fmt.Errorf("cannot build LSP URI")
	}
	langID := languageIDFromPath(buf.Path())
	if langID == "" {
		return nil, "", "", nil, fmt.Errorf("no language server mapping for %s", filepath.Base(buf.Path()))
	}
	client, err := a.lspClientForLanguage(langID)
	if err != nil {
		return nil, "", "", nil, err
	}
	return buf, uri, langID, client, nil
}

func (a *maneApp) activeCursorPosition(buf *editor.Buffer) (lsp.Position, error) {
	if buf == nil {
		return lsp.Position{}, fmt.Errorf("no active buffer")
	}
	text := buf.Text()
	cursorOffset := a.textArea.CursorOffset()
	byteOffset := runeOffsetToByteOffset(text, cursorOffset)
	return lspPositionFromByteOffset(text, byteOffset), nil
}

func (a *maneApp) setCursorFromLSPPosition(uri string, pos lsp.Position) error {
	path := filePathFromURI(uri)
	if path != "" && (a.tabs.ActiveBuffer() == nil || a.tabs.ActiveBuffer().Path() != path) {
		if _, err := a.tabs.OpenFile(path); err == nil {
			a.syncTextArea()
			a.syncTabBar()
			a.syncBreadcrumbs()
			a.highlight.setup(filepath.Base(path))
			a.openLSPDocument(a.tabs.ActiveBuffer())
			text := a.tabs.ActiveBuffer().Text()
			a.applyHighlights(text, a.highlight.highlight([]byte(text)))
			a.updateFoldRegions(text)
		} else {
			return err
		}
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return fmt.Errorf("no active buffer")
	}
	text := buf.Text()
	byteOffset := lspOffsetFromPosition(text, pos)
	mapping := byteOffsetToRuneOffset(text)
	runeOffset := mapping[byteOffset]
	a.ensureLineVisible(pos.Line)
	a.textArea.SetCursorOffset(runeOffset)
	a.updateStatus()
	return nil
}

func (a *maneApp) lspDiagnosticsForActive() []lsp.Diagnostic {
	uri := ""
	if buf := a.tabs.ActiveBuffer(); buf != nil {
		uri = fileURI(buf.Path())
	}
	if uri == "" {
		return nil
	}
	a.lspMu.Lock()
	defer a.lspMu.Unlock()
	out := make([]lsp.Diagnostic, 0, len(a.lspDiagnostics[uri]))
	out = append(out, a.lspDiagnostics[uri]...)
	return out
}

// applyHighlights converts gotreesitter.HighlightRange values to TextAreaHighlight
// and sets them on the TextArea.
func (a *maneApp) applyHighlights(text string, ranges []gotreesitter.HighlightRange) {
	if len(ranges) == 0 || a.theme == nil {
		a.syntaxHighlights = nil
		a.mergeAllHighlights()
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
	a.mergeAllHighlights()
}

// openFile opens a file by path through the TabManager and loads its content
// into the TextArea.
func (a *maneApp) openFile(path string) error {
	_, err := a.tabs.OpenFile(path)
	if err != nil {
		a.status.Set(fmt.Sprintf(" error: %v", err))
		return err
	}

	// Set up syntax highlighting for the file's language.
	a.highlight.setup(filepath.Base(path))

	a.syncTextArea()
	a.syncTabBar()
	a.syncBreadcrumbs()
	a.updateStatus()

	buf := a.tabs.ActiveBuffer()
	if buf != nil {
		text := buf.Text()
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
		a.openLSPDocument(buf)
		a.applyDiagnosticsForActiveBuffer()
		a.updateFoldRegions(text)
		a.updateStatus()
	}
	return nil
}

// syncTextArea loads the active buffer's text into the TextArea widget.
func (a *maneApp) syncTextArea() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		a.textArea.SetText("")
		a.bracketHighlights = nil
		a.multiHighlights = nil
		a.blockHighlights = nil
		if a.blockSelection != nil {
			a.blockSelection.Clear()
		}
		a.foldState.SetRegions(nil)
		if a.multiCursor == nil {
			a.multiCursor = editor.NewMultiCursor()
		} else {
			a.multiCursor.Reset()
		}
		a.textArea.SetVisibleLines(nil)
		a.applyDiagnosticsForActiveBuffer()
		return
	}
	a.textArea.SetText(buf.Text())
	a.syncMultiCursorFromTextArea()
	a.clearBlockSelection()
	a.applyDiagnosticsForActiveBuffer()
}

func clampRuneOffset(offset int, max int) int {
	if offset < 0 {
		return 0
	}
	if offset > max {
		return max
	}
	return offset
}

func (a *maneApp) isMultiCursorMode() bool {
	return a.multiCursor != nil && a.multiCursor.IsMulti()
}

func (a *maneApp) syncMultiCursorFromTextArea() {
	if a.multiCursor == nil {
		a.multiCursor = editor.NewMultiCursor()
	}

	text := a.textArea.Text()
	textLen := utf8.RuneCountInString(text)
	sel := a.textArea.GetSelection()

	a.multiCursor.Reset()
	a.multiCursor.SetPrimary(clampRuneOffset(a.textArea.CursorOffset(), textLen), clampRuneOffset(sel.Start, textLen))
	a.syncMultiHighlights()
}

func (a *maneApp) resetMultiCursor() {
	a.syncMultiCursorFromTextArea()
	a.updateStatus()
	a.mergeAllHighlights()
}

func (a *maneApp) syncTextAreaFromMultiCursor() {
	if a.multiCursor == nil {
		return
	}
	primary := a.multiCursor.Primary()
	textLen := utf8.RuneCountInString(a.textArea.Text())
	a.textArea.SetCursorOffset(clampRuneOffset(primary.Offset, textLen))
	a.textArea.SetSelection(widgets.Selection{
		Start: clampRuneOffset(primary.Anchor, textLen),
		End:   clampRuneOffset(primary.Offset, textLen),
	})
	a.syncMultiHighlights()
}

func (a *maneApp) applyMultiCursorText(newText string) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	buf.SetText(newText)
	a.suppressChange = true
	a.textArea.SetText(newText)
	a.suppressChange = false
	a.syncTextAreaFromMultiCursor()
	a.rehighlight(newText)
	a.scheduleLspDidChange(buf, newText)
	a.updateBracketMatch()
	a.updateStatus()
}

func (a *maneApp) applyMultiCursorInsert(text string) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	if !a.isMultiCursorMode() {
		a.syncMultiCursorFromTextArea()
	}

	newText := a.multiCursor.InsertAtAll(buf.Text(), text)
	a.applyMultiCursorText(newText)
}

func (a *maneApp) applyPaste(text string) {
	if text == "" {
		return
	}
	if a.isBlockSelectionMode() {
		a.applyBlockInsert(text)
		return
	}
	if a.isMultiCursorMode() {
		a.applyMultiCursorInsert(text)
		return
	}
	a.textArea.ClipboardPaste(text)
}

func (a *maneApp) clipboardText() string {
	cb := clipboard.NewAutoClipboard(os.Stdout)
	if cb == nil || !cb.Available() {
		return ""
	}
	text, err := cb.Read()
	if err != nil || text == "" {
		return ""
	}
	return text
}

func (a *maneApp) applyMultiCursorDeleteBackspace() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	newText := a.multiCursor.DeleteBackspace(buf.Text())
	a.applyMultiCursorText(newText)
}

func (a *maneApp) applyMultiCursorDeleteForward() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	newText := a.multiCursor.DeleteForward(buf.Text())
	a.applyMultiCursorText(newText)
}

func (a *maneApp) addNextCursorOccurrence() {
	if a.multiCursor == nil {
		a.multiCursor = editor.NewMultiCursor()
	}
	if !a.isMultiCursorMode() {
		a.syncMultiCursorFromTextArea()
	}

	if !a.multiCursor.AddNextOccurrence(a.textArea.Text()) {
		a.status.Set(" no further occurrences")
		return
	}

	a.syncTextAreaFromMultiCursor()
	a.updateStatus()
	a.mergeAllHighlights()
}

func (a *maneApp) syncMultiHighlights() {
	if a.multiCursor == nil || !a.isMultiCursorMode() {
		a.multiHighlights = nil
		return
	}

	textLen := utf8.RuneCountInString(a.textArea.Text())
	otherStyle := backend.DefaultStyle().Background(backend.ColorRGB(0x00, 0x5f, 0x87))

	highlights := make([]widgets.TextAreaHighlight, 0, a.multiCursor.Count())
	for i, c := range a.multiCursor.Cursors() {
		if i == 0 {
			continue
		}

		start := c.Anchor
		end := c.Offset
		if start > end {
			start, end = end, start
		}
		start = clampRuneOffset(start, textLen)
		end = clampRuneOffset(end, textLen)
		if start > end {
			start, end = end, start
		}
		if end == start {
			if start < textLen {
				end = start + 1
			} else if start > 0 {
				start = start - 1
				end = start + 1
			} else {
				continue
			}
		}
		highlights = append(highlights, widgets.TextAreaHighlight{
			Start: start,
			End:   end,
			Style: otherStyle,
		})
	}
	a.multiHighlights = highlights
}

func (a *maneApp) isBlockSelectionMode() bool {
	return a.blockSelection != nil && a.blockSelection.Active
}

func (a *maneApp) clearBlockSelection() {
	if a.blockSelection == nil || !a.blockSelection.Active {
		return
	}
	a.blockSelection.Clear()
	a.blockHighlights = nil
	a.mergeAllHighlights()
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (a *maneApp) maxLineColumn(text string) int {
	lines := strings.Split(text, "\n")
	maxCol := 0
	for _, line := range lines {
		if col := utf8.RuneCountInString(line); col > maxCol {
			maxCol = col
		}
	}
	return maxCol
}

func (a *maneApp) expandBlockSelection(deltaRow, deltaCol int) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil || a.blockSelection == nil {
		return
	}

	text := a.textArea.Text()
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return
	}
	maxRow := len(lines) - 1
	maxCol := a.maxLineColumn(text) + 1

	if !a.blockSelection.Active {
		col, row := a.textArea.CursorPosition()
		a.blockAnchorRow = clampInt(row, 0, maxRow)
		a.blockAnchorCol = clampInt(col, 0, maxCol)
		a.blockFocusRow = a.blockAnchorRow
		a.blockFocusCol = a.blockAnchorCol
		// Block selection and multi-cursor are mutually exclusive.
		a.syncMultiCursorFromTextArea()
	}

	a.blockFocusRow = clampInt(a.blockFocusRow+deltaRow, 0, maxRow)
	a.blockFocusCol = clampInt(a.blockFocusCol+deltaCol, 0, maxCol)
	a.blockSelection.Set(
		a.blockAnchorRow,
		a.blockFocusRow,
		a.blockAnchorCol,
		a.blockFocusCol,
	)
	a.textArea.SelectNone()
	a.syncBlockHighlights()
	a.updateStatus()
}

func (a *maneApp) syncBlockHighlights() {
	if !a.isBlockSelectionMode() {
		a.blockHighlights = nil
		return
	}

	text := a.textArea.Text()
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		a.blockHighlights = nil
		return
	}

	startLine, endLine := a.blockSelection.Lines()
	startCol, endCol := a.blockSelection.Cols()
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	if startLine > endLine {
		a.blockHighlights = nil
		return
	}

	// Pre-compute line-start rune offsets.
	lineStart := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		lineStart[i] = offset
		offset += utf8.RuneCountInString(line)
		if i < len(lines)-1 {
			offset++ // newline rune
		}
	}

	style := backend.DefaultStyle().Background(backend.ColorRGB(0x2f, 0x5f, 0x87))
	highlights := make([]widgets.TextAreaHighlight, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		runes := []rune(lines[line])
		start := startCol
		end := endCol
		if start > len(runes) {
			start = len(runes)
		}
		if end > len(runes) {
			end = len(runes)
		}
		if start > end {
			start = end
		}
		// Keep caret-visible highlight for zero-width column selections.
		if end == start {
			if start < len(runes) {
				end = start + 1
			} else {
				continue
			}
		}

		highlights = append(highlights, widgets.TextAreaHighlight{
			Start: lineStart[line] + start,
			End:   lineStart[line] + end,
			Style: style,
		})
	}
	a.blockHighlights = highlights
	a.mergeAllHighlights()
}

func (a *maneApp) applyBlockSelectionText(newText string, cursorCol, cursorRow int) {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	buf.SetText(newText)
	a.suppressChange = true
	a.textArea.SetText(newText)
	a.textArea.SetCursorPosition(cursorCol, cursorRow)
	a.textArea.SelectNone()
	a.suppressChange = false
	a.clearBlockSelection()
	a.syncMultiCursorFromTextArea()
	a.rehighlight(newText)
	a.scheduleLspDidChange(buf, newText)
	a.updateBracketMatch()
	a.updateStatus()
}

func (a *maneApp) applyBlockInsert(insert string) {
	if !a.isBlockSelectionMode() {
		return
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	bs := *a.blockSelection
	text := buf.Text()
	if bs.EndCol > bs.StartCol {
		text = bs.DeleteBlock(text)
		bs.EndCol = bs.StartCol
	}
	newText := bs.InsertAtBlock(text, insert)
	insertWidth := utf8.RuneCountInString(insert)
	a.applyBlockSelectionText(newText, bs.StartCol+insertWidth, bs.EndLine)
}

func (a *maneApp) applyBlockBackspace() {
	if !a.isBlockSelectionMode() {
		return
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	bs := *a.blockSelection
	text := buf.Text()
	cursorCol := bs.StartCol

	if bs.EndCol > bs.StartCol {
		text = bs.DeleteBlock(text)
	} else if bs.StartCol > 0 {
		bs.StartCol--
		bs.EndCol = bs.StartCol + 1
		text = bs.DeleteBlock(text)
		cursorCol = bs.StartCol
	} else {
		return
	}
	a.applyBlockSelectionText(text, cursorCol, bs.EndLine)
}

func (a *maneApp) applyBlockDeleteForward() {
	if !a.isBlockSelectionMode() {
		return
	}
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	bs := *a.blockSelection
	if bs.EndCol == bs.StartCol {
		bs.EndCol = bs.StartCol + 1
	}
	newText := bs.DeleteBlock(buf.Text())
	a.applyBlockSelectionText(newText, bs.StartCol, bs.EndLine)
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

	// Extra editor metadata.
	encoding := " UTF-8"
	lineEnding := detectLineEnding(buf.Text())
	indent := detectIndentMode(buf.Text())
	selectionCount := a.selectionCount()
	branch := a.currentGitBranch(buf.Path())
	wrap := "off"
	if a.wordWrap {
		wrap = "on"
	}

	col, row := a.textArea.CursorPosition()
	status := fmt.Sprintf(
		" %s%s%s  Ln %d, Col %d  %s  %s  %s",
		buf.Title(),
		dirty,
		langName,
		row+1,
		col+1,
		encoding,
		lineEnding,
		indent,
	)
	status += fmt.Sprintf("  wrap:%s", wrap)
	if branch != "" {
		status += "  " + branch
	}
	if selectionCount > 0 {
		status += fmt.Sprintf("  Sel %d", selectionCount)
		if a.isMultiCursorMode() {
			status += fmt.Sprintf(" (%d cursors)", a.multiCursor.Count())
		} else if a.isBlockSelectionMode() {
			status += " (block)"
		}
	}
	if diagSummary := a.diagnosticSummary(); diagSummary != "" {
		status += "  " + diagSummary
	}
	a.status.Set(status)
	a.syncBreadcrumbs()
}

// selectionCount returns the currently selected rune count in the editor.
func (a *maneApp) selectionCount() int {
	if a.isBlockSelectionMode() {
		return len(strings.Join(a.blockSelection.ExtractBlock(a.textArea.Text()), ""))
	}

	type interval struct {
		Start int
		End   int
	}

	var cursors []editor.Cursor
	if a.multiCursor != nil && a.multiCursor.IsMulti() {
		cursors = a.multiCursor.Cursors()
	} else {
		sel := a.textArea.GetSelection()
		cursors = []editor.Cursor{{Offset: a.textArea.CursorOffset(), Anchor: sel.Start}}
	}

	textLen := utf8.RuneCountInString(a.textArea.Text())
	ranges := make([]interval, 0, len(cursors))

	for _, c := range cursors {
		start := c.Offset
		end := c.Anchor
		if start > end {
			start, end = end, start
		}
		start = clampRuneOffset(start, textLen)
		end = clampRuneOffset(end, textLen)
		if start == end {
			continue
		}
		ranges = append(ranges, interval{Start: start, End: end})
	}

	if len(ranges) == 0 {
		return 0
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start == ranges[j].Start {
			return ranges[i].End < ranges[j].End
		}
		return ranges[i].Start < ranges[j].Start
	})

	merged := make([]interval, 0, len(ranges))
	for _, r := range ranges {
		if len(merged) == 0 {
			merged = append(merged, r)
			continue
		}
		last := &merged[len(merged)-1]
		if r.Start > last.End {
			merged = append(merged, r)
			continue
		}
		if r.End > last.End {
			last.End = r.End
		}
	}

	total := 0
	for _, r := range merged {
		total += r.End - r.Start
	}
	return total
}

func nodeContainsPoint(node *gotreesitter.Node, row, col int) bool {
	if node == nil {
		return false
	}
	start := node.StartPoint()
	end := node.EndPoint()
	if row < int(start.Row) || row > int(end.Row) {
		return false
	}
	if row == int(start.Row) && col < int(start.Column) {
		return false
	}
	if row == int(end.Row) && col > int(end.Column) {
		return false
	}
	return true
}

func symbolPathAtPoint(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, row, col int) []string {
	if root == nil || lang == nil {
		return nil
	}

	path := make([]string, 0, 8)
	var walk func(node *gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil || !nodeContainsPoint(node, row, col) {
			return
		}

		if kind := symbolKindFromNodeType(node.Type(lang)); kind != "" {
			if name := extractSymbolName(node, lang, source); name != "" {
				path = append(path, name)
			}
		}

		for i := 0; i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if nodeContainsPoint(child, row, col) {
				walk(child)
				return
			}
		}
	}

	walk(root)
	return path
}

func (a *maneApp) currentSymbolPath(text string, row, col int) []string {
	a.highlight.mu.Lock()
	tree := a.highlight.tree
	lang := a.highlight.lang
	a.highlight.mu.Unlock()
	if tree == nil || lang == nil {
		return nil
	}
	return symbolPathAtPoint(tree.RootNode(), lang, []byte(text), row, col)
}

// syncBreadcrumbs rebuilds breadcrumb links from the active file path.
func (a *maneApp) syncBreadcrumbs() {
	if a.breadcrumbs == nil {
		return
	}

	buf := a.tabs.ActiveBuffer()
	if buf == nil || buf.Path() == "" {
		a.breadcrumbs.Items = nil
		return
	}

	clean := filepath.Clean(buf.Path())
	separator := string(filepath.Separator)
	parts := strings.Split(clean, separator)

	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			normalized = append(normalized, part)
		}
	}

	if len(normalized) == 0 && !filepath.IsAbs(clean) {
		a.breadcrumbs.Items = nil
		return
	}

	// Build up visible path segments from root.
	items := make([]widgets.BreadcrumbItem, 0, len(normalized)+1)

	cursor := ""
	if filepath.IsAbs(clean) {
		vol := filepath.VolumeName(clean)
		if vol != "" {
			cursor = vol + separator
		} else {
			cursor = separator
		}
		rootPath := cursor
		items = append(items, widgets.BreadcrumbItem{
			Label: rootPath,
			OnClick: func(path string) func() {
				return func() {
					a.fileTree.SetRoot(path)
				}
			}(rootPath),
		})
		if vol != "" && len(normalized) > 0 && normalized[0] == vol {
			normalized = normalized[1:]
		}
	}

	for i, part := range normalized {
		if cursor == "" {
			cursor = part
		} else {
			cursor = filepath.Join(cursor, part)
		}

		path := cursor
		isFile := i == len(normalized)-1
		item := widgets.BreadcrumbItem{
			Label: part,
			OnClick: func(p string, dir bool) func() {
				return func() {
					if dir {
						a.fileTree.SetRoot(p)
						return
					}
					a.fileTree.SetRoot(filepath.Dir(p))
				}
			}(path, !isFile),
		}
		items = append(items, item)
	}

	col, row := a.textArea.CursorPosition()
	for _, symbol := range a.currentSymbolPath(buf.Text(), row, col) {
		items = append(items, widgets.BreadcrumbItem{Label: symbol})
	}

	if len(items) == 0 {
		// Path is filesystem root.
		rootPath := cursor
		items = append(items, widgets.BreadcrumbItem{
			Label: rootPath,
			OnClick: func(path string) func() {
				return func() {
					a.fileTree.SetRoot(path)
				}
			}(rootPath),
		})
	}

	a.breadcrumbs.Items = items
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

// fileFinderRoot returns the active tree root used for file searching.
func (a *maneApp) fileFinderRoot() string {
	if a.fileTree != nil {
		if root := a.fileTree.Root(); root != "" {
			return root
		}
	}
	return a.treeRoot
}

// cmdOpenFileFinder opens the fuzzy file finder overlay.
func (a *maneApp) cmdOpenFileFinder() runtime.HandleResult {
	if a.fileFinder == nil {
		return runtime.Handled()
	}
	root := a.fileFinderRoot()
	if root == "" {
		return runtime.Handled()
	}

	if a.finderRoot != root || len(a.finderPath) == 0 {
		items, err := collectFinderFiles(root)
		if err != nil {
			a.status.Set(fmt.Sprintf(" file finder error: %v", err))
			return runtime.Handled()
		}
		a.finderRoot = root
		a.finderPath = items
	}

	a.fileFinder.SetFiles(a.finderPath)
	a.fileFinder.Show()
	a.fileFinder.Focus()
	return runtime.Handled()
}

func (a *maneApp) onFileFinder(path string) {
	if path == "" {
		return
	}
	_ = a.openFile(path)
}

func (a *maneApp) onFileFinderClose() {
	// Keep status behavior unchanged when the finder is dismissed.
}

// cmdShowPalette opens the command palette.
func (a *maneApp) cmdShowPalette() {
	a.palette.Toggle()
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
		a.updateFoldRegions(text)
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
		a.updateFoldRegions(text)
	})
}

// onReplaceClose clears search state when the replace widget is dismissed.
func (a *maneApp) onReplaceClose() {
	a.searchMatches = nil
	a.searchCurrent = 0
	a.textArea.SetHighlights(a.syntaxHighlights)
}

// cmdGotoLine opens the go-to-line input overlay.
func (a *maneApp) cmdGotoLine() runtime.HandleResult {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return runtime.Unhandled()
	}
	_, row := a.textArea.CursorPosition()
	a.gotoLineW.SetQuery(strconv.Itoa(row + 1))
	a.gotoLineW.Focus()
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.gotoLineW})
}

// onGotoLine handles the submitted line number from the go-to-line widget.
func (a *maneApp) onGotoLine(line int) {
	if line <= 0 {
		a.status.Set(" Invalid line number")
		return
	}

	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}

	maxLine := editor.LineCount(buf.Text())
	if maxLine == 0 {
		maxLine = 1
	}
	if line > maxLine {
		line = maxLine
	}

	a.ensureLineVisible(line - 1)
	a.textArea.SetCursorPosition(0, line-1)
	a.updateStatus()
}

func (a *maneApp) onGotoLineClose() {
	// Keep status behavior unchanged when the overlay is dismissed.
}

func (a *maneApp) currentGitBranch(path string) string {
	if path == "" {
		return ""
	}
	if a.gitBranchPath == path && a.gitBranchCache != "" {
		return a.gitBranchCache
	}
	a.gitBranchPath = path
	a.gitBranchCache = detectGitBranch(path)
	return a.gitBranchCache
}

// cmdGotoMatchingBracket jumps the cursor to the matching bracket if one is
// nearby the cursor position.
func (a *maneApp) cmdGotoMatchingBracket() {
	text := a.textArea.Text()
	offset := a.textArea.CursorOffset()
	for _, pos := range []int{offset, offset - 1} {
		if match, ok := editor.FindMatchingBracket(text, pos); ok {
			a.textArea.SetCursorOffset(match)
			a.updateStatus()
			return
		}
	}
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
	pos := lspPositionFromByteOffset(text, m.Start)
	a.ensureLineVisible(pos.Line)
	a.textArea.SetCursorOffset(runeStart)
}

// applySearchHighlights merges syntax highlights with search match highlights.
func (a *maneApp) applySearchHighlights() {
	a.mergeAllHighlights()
}

func (a *maneApp) ensureLSPPalette() {
	if a.lspPalette == nil {
		a.lspPalette = widgets.NewCommandPalette()
	}
}

func (a *maneApp) showLSPPalette(commands []widgets.PaletteCommand, status string) {
	a.ensureLSPPalette()
	if len(commands) == 0 {
		a.lspPalette.Hide()
		a.status.Set(" " + status)
		return
	}
	a.lspPalette.SetCommands(commands)
	a.lspPalette.Show()
	a.lspPalette.Focus()
	a.status.Set(" " + status)
}

func (a *maneApp) lspHoverContent() (string, error) {
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		return "", err
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		return "", err
	}

	var hover *lsp.Hover
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		hover, callErr = client.HoverInfo(a.lspCtx, uri, pos)
		return callErr
	})
	if err != nil {
		return "", err
	}
	if hover == nil {
		return "", nil
	}
	return strings.TrimSpace(extractHoverText(hover.Contents)), nil
}

func (a *maneApp) cmdLspHoverPanel() runtime.HandleResult {
	content, err := a.lspHoverContent()
	if err != nil {
		a.status.Set(fmt.Sprintf(" hover failed: %v", err))
		return runtime.Handled()
	}
	if content == "" {
		a.status.Set(" no hover info")
		return runtime.Handled()
	}

	label := content
	if idx := strings.IndexRune(label, '\n'); idx >= 0 {
		label = label[:idx]
	}
	if len(label) > 80 {
		label = label[:77] + "..."
	}

	a.showLSPPalette([]widgets.PaletteCommand{
		{
			ID:          "lsp.hover",
			Label:       label,
			Description: strings.ReplaceAll(content, "\n", " "),
		},
	}, "Hover")
	return runtime.Handled()
}

func (a *maneApp) cmdLspComplete() {
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}

	var items []lsp.CompletionItem
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		items, callErr = client.Completion(a.lspCtx, uri, pos)
		return callErr
	})
	if err != nil {
		a.status.Set(fmt.Sprintf(" completion failed: %v", err))
		return
	}
	if len(items) == 0 {
		a.status.Set(" no completion suggestions")
		return
	}

	cmds := make([]widgets.PaletteCommand, 0, len(items))
	for i, item := range items {
		insert := item.InsertText
		if insert == "" {
			insert = item.Label
		}
		if insert == "" {
			continue
		}
		description := item.Detail
		if description == "" {
			description = extractHoverText(item.Documentation)
		}
		itemCopy := item
		cmds = append(cmds, widgets.PaletteCommand{
			ID:          fmt.Sprintf("lsp.complete.%d", i),
			Label:       item.Label,
			Description: description,
			OnExecute: func(item lsp.CompletionItem) func() {
				return func() {
					a.applyLSPCompletion(item)
					a.lspPalette.Hide()
				}
			}(itemCopy),
		})
	}
	if len(cmds) == 0 {
		a.status.Set(" no completion suggestions")
		return
	}
	a.showLSPPalette(cmds, fmt.Sprintf("Completion: %d items", len(cmds)))
}

func parseSnippetPlaceholder(token string) (index int, text string, ok bool) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) == 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", false
	}
	if len(parts) == 2 {
		return n, parts[1], true
	}
	return n, "", true
}

func expandSnippetInsert(snippet string) (string, int) {
	var out strings.Builder
	runeLen := 0
	firstTabStop := -1
	finalTabStop := -1

	appendText := func(s string) {
		if s == "" {
			return
		}
		out.WriteString(s)
		runeLen += utf8.RuneCountInString(s)
	}

	for i := 0; i < len(snippet); {
		if snippet[i] == '\\' && i+1 < len(snippet) {
			next := snippet[i+1]
			if next == '$' || next == '{' || next == '}' || next == '\\' {
				appendText(snippet[i+1 : i+2])
				i += 2
				continue
			}
		}

		if snippet[i] != '$' {
			_, size := utf8.DecodeRuneInString(snippet[i:])
			if size <= 0 {
				size = 1
			}
			appendText(snippet[i : i+size])
			i += size
			continue
		}

		// Braced placeholder: ${1:name}, ${2}, ${0}
		if i+1 < len(snippet) && snippet[i+1] == '{' {
			end := i + 2
			for end < len(snippet) && snippet[end] != '}' {
				end++
			}
			if end >= len(snippet) {
				appendText(snippet[i : i+1])
				i++
				continue
			}

			idx, placeholderText, ok := parseSnippetPlaceholder(snippet[i+2 : end])
			if !ok {
				appendText(snippet[i : end+1])
				i = end + 1
				continue
			}
			if idx == 0 {
				if finalTabStop < 0 {
					finalTabStop = runeLen
				}
			} else if firstTabStop < 0 {
				firstTabStop = runeLen
			}
			appendText(placeholderText)
			i = end + 1
			continue
		}

		// Numeric tab stop: $1, $0.
		if i+1 < len(snippet) && snippet[i+1] >= '0' && snippet[i+1] <= '9' {
			j := i + 1
			for j < len(snippet) && snippet[j] >= '0' && snippet[j] <= '9' {
				j++
			}
			idx, _ := strconv.Atoi(snippet[i+1 : j])
			if idx == 0 {
				if finalTabStop < 0 {
					finalTabStop = runeLen
				}
			} else if firstTabStop < 0 {
				firstTabStop = runeLen
			}
			i = j
			continue
		}

		appendText(snippet[i : i+1])
		i++
	}

	text := out.String()
	switch {
	case firstTabStop >= 0:
		return text, firstTabStop
	case finalTabStop >= 0:
		return text, finalTabStop
	default:
		return text, runeLen
	}
}

func (a *maneApp) applyLSPCompletion(item lsp.CompletionItem) {
	insert := item.InsertText
	if insert == "" {
		insert = item.Label
	}
	if insert == "" {
		return
	}

	cursorDelta := utf8.RuneCountInString(insert)
	if item.InsertTextFormat == 2 {
		insert, cursorDelta = expandSnippetInsert(insert)
	}

	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	text := buf.Text()
	offsetRunes := a.textArea.CursorOffset()
	offsetBytes := runeOffsetToByteOffset(text, offsetRunes)
	newText := text[:offsetBytes] + insert + text[offsetBytes:]

	a.suppressChange = true
	buf.SetText(newText)
	a.textArea.SetText(newText)
	a.suppressChange = false
	a.textArea.SetCursorOffset(offsetRunes + cursorDelta)
	a.textArea.SetSelection(widgets.Selection{})
	a.syncMultiCursorFromTextArea()
	a.rehighlight(newText)
	a.scheduleLspDidChange(buf, newText)
	a.applyDiagnosticsForActiveBuffer()
	a.status.Set(fmt.Sprintf(" inserted: %s", item.Label))
	a.updateStatus()
}

func (a *maneApp) cmdLspDefinition() {
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}

	var locations []lsp.Location
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		locations, callErr = client.Definition(a.lspCtx, uri, pos)
		return callErr
	})
	if err != nil {
		a.status.Set(fmt.Sprintf(" definition lookup failed: %v", err))
		return
	}
	if len(locations) == 0 {
		a.status.Set(" no definition")
		return
	}

	if err := a.openLSPLocation(locations[0].URI, locations[0].Range.Start); err != nil {
		a.status.Set(fmt.Sprintf(" definition failed: %v", err))
		return
	}
}

func (a *maneApp) cmdLspReferences() {
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}

	var locations []lsp.Location
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		locations, callErr = client.References(a.lspCtx, uri, pos)
		return callErr
	})
	if err != nil {
		a.status.Set(fmt.Sprintf(" references lookup failed: %v", err))
		return
	}
	if len(locations) == 0 {
		a.status.Set(" no references")
		return
	}

	cmds := make([]widgets.PaletteCommand, 0, len(locations))
	for i, loc := range locations {
		path := filePathFromURI(loc.URI)
		displayPath := path
		if path == "" {
			displayPath = loc.URI
		}
		title := fmt.Sprintf("%s:%d:%d", filepath.Base(displayPath), loc.Range.Start.Line+1, loc.Range.Start.Character+1)
		cmds = append(cmds, widgets.PaletteCommand{
			ID:          fmt.Sprintf("lsp.ref.%d", i),
			Label:       title,
			Description: displayPath,
			OnExecute: func(uri string, pos lsp.Position) func() {
				return func() {
					if err := a.openLSPLocation(uri, pos); err != nil {
						a.status.Set(fmt.Sprintf(" reference open failed: %v", err))
					}
				}
			}(loc.URI, loc.Range.Start),
		})
	}

	a.showLSPPalette(cmds, fmt.Sprintf("References: %d", len(cmds)))
}

func (a *maneApp) cmdLspHover() {
	content, err := a.lspHoverContent()
	if err != nil {
		a.status.Set(fmt.Sprintf(" hover failed: %v", err))
		return
	}
	if content == "" {
		a.status.Set(" no hover info")
		return
	}
	if len(content) > 160 {
		content = content[:157] + "..."
	}
	a.status.Set(" " + strings.ReplaceAll(content, "\n", " "))
}

func (a *maneApp) cmdLspDiagnostics() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		a.status.Set(" no active buffer")
		return
	}

	diags := a.lspDiagnosticsForActive()
	if len(diags) == 0 {
		a.status.Set(" no diagnostics")
		return
	}

	a.ensureLSPPalette()
	cmds := make([]widgets.PaletteCommand, 0, len(diags))
	for i, diag := range diags {
		sev := "I"
		switch diag.Severity {
		case 1:
			sev = "E"
		case 2:
			sev = "W"
		case 4:
			sev = "H"
		}

		msg := strings.ReplaceAll(strings.TrimSpace(diag.Message), "\n", " ")
		if len(msg) > 90 {
			msg = msg[:87] + "..."
		}
		source := strings.TrimSpace(diag.Source)
		if source == "" {
			source = filepath.Base(buf.Path())
		}

		start := diag.Range.Start
		cmds = append(cmds, widgets.PaletteCommand{
			ID:          fmt.Sprintf("lsp.diag.%d", i),
			Label:       fmt.Sprintf("[%s] Ln %d, Col %d", sev, start.Line+1, start.Character+1),
			Description: fmt.Sprintf("%s: %s", source, msg),
			OnExecute: func(pos lsp.Position) func() {
				return func() {
					a.ensureLineVisible(pos.Line)
					a.textArea.SetCursorPosition(pos.Character, pos.Line)
					a.lspPalette.Hide()
					a.updateStatus()
				}
			}(start),
		})
	}

	a.showLSPPalette(cmds, fmt.Sprintf("Diagnostics: %d", len(cmds)))
}

func (a *maneApp) cmdLspCodeAction() {
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	diags := a.lspDiagnosticsForActive()
	var actions []lsp.CodeAction
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		actions, callErr = client.CodeAction(a.lspCtx, uri, lsp.Range{Start: pos, End: pos}, diags)
		return callErr
	})
	if err != nil {
		a.status.Set(fmt.Sprintf(" code action failed: %v", err))
		return
	}
	if len(actions) == 0 {
		a.status.Set(" no code actions")
		return
	}

	cmds := make([]widgets.PaletteCommand, 0, len(actions))
	for i, action := range actions {
		title := action.Title
		if title == "" {
			title = "Code Action"
		}
		actionCopy := action
		cmds = append(cmds, widgets.PaletteCommand{
			ID:    fmt.Sprintf("lsp.action.%d", i),
			Label: title,
			OnExecute: func(action lsp.CodeAction) func() {
				return func() {
					if action.Edit == nil || len(action.Edit.Changes) == 0 {
						a.status.Set(" selected code action has no edits")
						return
					}
					if err := a.applyWorkspaceEdits(action.Edit.Changes); err != nil {
						a.status.Set(fmt.Sprintf(" apply code action failed: %v", err))
					} else {
						a.status.Set(" applied code action")
					}
				}
			}(actionCopy),
		})
	}
	a.showLSPPalette(cmds, fmt.Sprintf("Code actions: %d", len(cmds)))
}

func (a *maneApp) cmdLspRename() runtime.HandleResult {
	if _, _, _, _, err := a.activeLSPSession(); err != nil {
		a.status.Set(" " + err.Error())
		return runtime.Handled()
	}
	a.renameW.SetText(a.currentSymbolAtCursor())
	a.renameW.Focus()
	return runtime.WithCommand(runtime.PushOverlay{Widget: a.renameW})
}

func (a *maneApp) cmdRenameSubmit(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		a.status.Set(" rename cancelled")
		return
	}
	buf, uri, langID, _, err := a.activeLSPSession()
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}
	pos, err := a.activeCursorPosition(buf)
	if err != nil {
		a.status.Set(" " + err.Error())
		return
	}

	var changes map[string][]lsp.TextEdit
	err = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		var callErr error
		changes, callErr = client.Rename(a.lspCtx, uri, pos, name)
		return callErr
	})
	if err != nil {
		a.status.Set(fmt.Sprintf(" rename failed: %v", err))
		return
	}
	if err := a.applyWorkspaceEdits(changes); err != nil {
		a.status.Set(fmt.Sprintf(" rename failed: %v", err))
		return
	}
	a.status.Set(fmt.Sprintf(" renamed to %q", name))
	a.updateStatus()
}

func (a *maneApp) openLSPLocation(uri string, pos lsp.Position) error {
	path := filePathFromURI(uri)
	if path == "" {
		return fmt.Errorf("invalid URI: %s", uri)
	}
	if err := a.openFile(path); err != nil {
		return err
	}
	return a.setCursorFromLSPPosition(uri, pos)
}

func (a *maneApp) findBufferByPath(path string) *editor.Buffer {
	if path == "" {
		return nil
	}
	normalized, err := filepath.Abs(path)
	if err != nil {
		normalized = filepath.Clean(path)
	}

	for _, buf := range a.tabs.Buffers() {
		if buf == nil || buf.Path() == "" {
			continue
		}
		bufferPath, err := filepath.Abs(buf.Path())
		if err != nil {
			bufferPath = filepath.Clean(buf.Path())
		}
		if filepath.Clean(bufferPath) == filepath.Clean(normalized) {
			return buf
		}
	}
	return nil
}

func (a *maneApp) restoreActiveBuffer(index int, cursorOffset int) {
	count := a.tabs.Count()
	if count == 0 {
		a.syncTextArea()
		a.syncTabBar()
		a.syncBreadcrumbs()
		a.applyDiagnosticsForActiveBuffer()
		return
	}
	if index < 0 {
		return
	}
	if index >= count {
		index = count - 1
	}
	a.tabs.SetActive(index)
	a.syncTextArea()
	a.syncTabBar()
	a.syncBreadcrumbs()
	maxOffset := utf8.RuneCountInString(a.textArea.Text())
	if cursorOffset < 0 {
		cursorOffset = 0
	}
	if cursorOffset > maxOffset {
		cursorOffset = maxOffset
	}
	a.textArea.SetCursorOffset(cursorOffset)
	a.syncMultiCursorFromTextArea()
	a.applyDiagnosticsForActiveBuffer()
	a.updateStatus()
}

func (a *maneApp) applyWorkspaceEdits(changes map[string][]lsp.TextEdit) error {
	if len(changes) == 0 {
		return fmt.Errorf("no edits to apply")
	}

	originalActiveIndex := a.tabs.Active()
	originalBuffer := a.tabs.ActiveBuffer()
	originalCursorOffset := 0
	if active := a.tabs.ActiveBuffer(); active != nil {
		originalCursorOffset = a.textArea.CursorOffset()
	}

	changed := false
	skipped := 0
	editedBuffers := 0

	for uri, edits := range changes {
		if len(edits) == 0 {
			continue
		}

		path := filePathFromURI(uri)
		if path == "" {
			skipped++
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}

		buf := a.findBufferByPath(path)
		if buf == nil {
			// Open non-active file to apply cross-file workspace edits.
			if _, err := a.tabs.OpenFile(path); err != nil {
				skipped++
				continue
			}
			buf = a.tabs.ActiveBuffer()
			if buf == nil {
				skipped++
				continue
			}
			a.openLSPDocument(buf)
		}
		if buf.Path() == "" {
			skipped++
			continue
		}

		text := buf.Text()
		type replacement struct {
			Start int
			End   int
			Text  string
		}
		ranges := make([]replacement, 0, len(edits))
		for _, edit := range edits {
			start, end := lspRangeToByteOffsets(text, edit.Range)
			ranges = append(ranges, replacement{
				Start: start,
				End:   end,
				Text:  edit.NewText,
			})
		}
		sort.Slice(ranges, func(i, j int) bool {
			if ranges[i].Start == ranges[j].Start {
				return ranges[i].End > ranges[j].End
			}
			return ranges[i].Start > ranges[j].Start
		})

		newText := text
		for _, r := range ranges {
			if r.Start < 0 || r.End < r.Start || r.Start > len(newText) {
				continue
			}
			if r.End > len(newText) {
				r.End = len(newText)
			}
			newText = newText[:r.Start] + r.Text + newText[r.End:]
		}

		a.suppressChange = true
		buf.SetText(newText)
		a.suppressChange = false
		a.scheduleLspDidChange(buf, newText)
		if buf == originalBuffer {
			a.textArea.SetText(newText)
			a.syncMultiCursorFromTextArea()
			a.rehighlight(newText)
		}
		changed = true
		editedBuffers++
	}

	a.restoreActiveBuffer(originalActiveIndex, originalCursorOffset)

	if !changed {
		if skipped > 0 {
			return fmt.Errorf("no edits applied (%d skipped)", skipped)
		}
		return fmt.Errorf("no edits to apply")
	}
	if skipped > 0 {
		a.status.Set(fmt.Sprintf(" applied edits in %d file(s); %d file(s) skipped", editedBuffers, skipped))
	}
	return nil
}

func (a *maneApp) currentSymbolAtCursor() string {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return ""
	}
	text := []rune(buf.Text())
	if len(text) == 0 {
		return ""
	}

	offset := a.textArea.CursorOffset()
	if offset < 0 {
		offset = 0
	}
	if offset >= len(text) {
		offset = len(text) - 1
	}
	if offset < 0 {
		return ""
	}

	isWord := func(r rune) bool {
		return r == '_' || r == '$' || ('0' <= r && r <= '9') || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')
	}
	if !isWord(text[offset]) {
		return ""
	}

	start := offset
	for start > 0 && isWord(text[start-1]) {
		start--
	}
	end := offset + 1
	for end < len(text) && isWord(text[end]) {
		end++
	}
	return string(text[start:end])
}

func extractHoverText(contents interface{}) string {
	switch v := contents.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]interface{}:
		if value, ok := v["value"].(string); ok && value != "" {
			return strings.TrimSpace(value)
		}
		if value, ok := v["kind"].(string); ok && value != "" {
			return strings.TrimSpace(value)
		}
	case []interface{}:
		for _, item := range v {
			if text := extractHoverText(item); text != "" {
				return text
			}
		}
	}
	return ""
}

// updateBracketMatch updates bracket highlight state based on the current
// cursor position. It checks the character at the cursor and the character
// before the cursor for bracket characters.
func (a *maneApp) updateBracketMatch() {
	text := a.textArea.Text()
	offset := a.textArea.CursorOffset()

	a.bracketHighlights = nil

	bracketStyle := backend.DefaultStyle().Background(backend.ColorRGB(0x44, 0x44, 0x44))

	// Check character at cursor and before cursor
	runes := []rune(text)
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

// mergeAllHighlights combines all highlight layers (syntax, brackets, diagnostics, search)
// and sets the merged result on the TextArea.
func (a *maneApp) mergeAllHighlights() {
	var merged []widgets.TextAreaHighlight
	merged = append(merged, a.syntaxHighlights...)
	merged = append(merged, a.bracketHighlights...)
	merged = append(merged, a.diagnostics...)
	merged = append(merged, a.multiHighlights...)
	merged = append(merged, a.blockHighlights...)
	// If search is active, add search highlights on top
	if len(a.searchMatches) > 0 {
		text := a.textArea.Text()
		mapping := byteOffsetToRuneOffset(text)
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
	}
	a.textArea.SetHighlights(merged)
}

func (a *maneApp) applyDiagnosticsForActiveBuffer() {
	text := a.textArea.Text()
	uri := ""
	if buf := a.tabs.ActiveBuffer(); buf != nil {
		uri = fileURI(buf.Path())
	}
	if uri == "" {
		a.lspMu.Lock()
		a.diagnostics = nil
		a.lspMu.Unlock()
		a.mergeAllHighlights()
		return
	}

	a.lspMu.Lock()
	diagnostics := append([]lsp.Diagnostic(nil), a.lspDiagnostics[uri]...)
	a.lspMu.Unlock()

	mapping := byteOffsetToRuneOffset(text)
	rendered := make([]widgets.TextAreaHighlight, 0, len(diagnostics))
	for _, diag := range diagnostics {
		start := lspOffsetFromPosition(text, diag.Range.Start)
		end := lspOffsetFromPosition(text, diag.Range.End)
		if start < 0 {
			start = 0
		}
		if end < start {
			end = start
		}
		if start > len(text) {
			start = len(text)
		}
		if end > len(text) {
			end = len(text)
		}

		style := backend.DefaultStyle().Underline(true).Foreground(backend.ColorYellow)
		if diag.Severity == 1 {
			style = style.Foreground(backend.ColorRed)
		} else if diag.Severity == 2 {
			style = style.Foreground(backend.ColorYellow)
		} else {
			style = style.Foreground(backend.ColorCyan)
		}
		rendered = append(rendered, widgets.TextAreaHighlight{
			Start: mapping[start],
			End:   mapping[end],
			Style: style,
		})
	}

	a.lspMu.Lock()
	a.diagnostics = rendered
	a.lspMu.Unlock()
	a.mergeAllHighlights()
}

func (a *maneApp) diagnosticSummary() string {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return ""
	}
	uri := fileURI(buf.Path())
	if uri == "" {
		return ""
	}

	a.lspMu.Lock()
	diags := a.lspDiagnostics[uri]
	a.lspMu.Unlock()

	errorCount := 0
	warningCount := 0
	for _, d := range diags {
		switch d.Severity {
		case 1:
			errorCount++
		case 2:
			warningCount++
		}
	}
	if errorCount == 0 && warningCount == 0 {
		return ""
	}

	if warningCount == 0 {
		return fmt.Sprintf("Diag: %d errors", errorCount)
	}
	if errorCount == 0 {
		return fmt.Sprintf("Diag: %d warnings", warningCount)
	}
	return fmt.Sprintf("Diag: %d errors, %d warnings", errorCount, warningCount)
}

func (a *maneApp) shutdownLSP() {
	a.lspMu.Lock()
	clients := make([]*lsp.Client, 0, len(a.lspClients))
	for _, client := range a.lspClients {
		clients = append(clients, client)
	}
	a.lspClients = make(map[string]*lsp.Client)
	a.lspDocVersions = make(map[string]int)
	a.lspDiagnostics = make(map[string][]lsp.Diagnostic)
	a.lspMu.Unlock()

	for _, client := range clients {
		_ = client.Close()
	}
}

func (a *maneApp) lspClientForLanguage(langID string) (*lsp.Client, error) {
	if langID == "" {
		return nil, fmt.Errorf("missing language id")
	}
	config, ok := a.lspServers[langID]
	if !ok || config.Command == "" {
		return nil, fmt.Errorf("no LSP server config for %s", langID)
	}

	a.lspMu.Lock()
	existing := a.lspClients[langID]
	a.lspMu.Unlock()
	if existing != nil {
		return existing, nil
	}

	client, err := lsp.NewClient(a.lspCtx, config.Command, config.Args...)
	if err != nil {
		return nil, err
	}

	client.SetNotifyHandler(func(method string, params json.RawMessage) {
		if method != "textDocument/publishDiagnostics" {
			return
		}
		var payload struct {
			URI         string `json:"uri"`
			Diagnostics []lsp.Diagnostic
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return
		}
		a.lspMu.Lock()
		a.lspDiagnostics[payload.URI] = payload.Diagnostics
		a.lspMu.Unlock()
	})

	if err := client.Initialize(a.lspCtx, fileURI(a.treeRoot)); err != nil {
		_ = client.Close()
		return nil, err
	}

	a.lspMu.Lock()
	existing = a.lspClients[langID]
	if existing != nil {
		// A concurrent initialization won the race.
		a.lspMu.Unlock()
		_ = client.Close()
		return existing, nil
	}
	a.lspClients[langID] = client
	a.lspMu.Unlock()
	return client, nil
}

func isLSPTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"client closed",
		"client is closed",
		"broken pipe",
		"connection reset",
		"eof",
		"read/write on closed pipe",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (a *maneApp) resetLSPClient(langID string) {
	if langID == "" {
		return
	}
	a.lspMu.Lock()
	client := a.lspClients[langID]
	delete(a.lspClients, langID)
	// Force DidOpen re-sync for docs in this language.
	for _, buf := range a.tabs.Buffers() {
		if buf == nil || buf.Path() == "" {
			continue
		}
		if languageIDFromPath(buf.Path()) != langID {
			continue
		}
		delete(a.lspDocVersions, fileURI(buf.Path()))
	}
	a.lspMu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func (a *maneApp) withRetryingLSPClient(langID string, op func(*lsp.Client) error) error {
	client, err := a.lspClientForLanguage(langID)
	if err != nil {
		return err
	}
	if err := op(client); err == nil {
		return nil
	} else if !isLSPTransportError(err) {
		return err
	}

	a.resetLSPClient(langID)
	client, err = a.lspClientForLanguage(langID)
	if err != nil {
		return err
	}
	return op(client)
}

func (a *maneApp) openLSPDocument(buf *editor.Buffer) {
	if buf == nil || buf.Path() == "" || a.lspCtx == nil {
		return
	}
	uri := fileURI(buf.Path())
	langID := languageIDFromPath(buf.Path())
	if uri == "" || langID == "" {
		return
	}

	a.lspMu.Lock()
	_, exists := a.lspDocVersions[uri]
	a.lspMu.Unlock()
	if exists {
		return
	}

	_ = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		if err := client.DidOpen(uri, langID, 1, buf.Text()); err != nil {
			return err
		}
		a.lspMu.Lock()
		a.lspDocVersions[uri] = 1
		a.lspMu.Unlock()
		return nil
	})
}

func (a *maneApp) scheduleLspDidChange(buf *editor.Buffer, text string) {
	if buf == nil || buf.Path() == "" || a.lspCtx == nil {
		return
	}

	uri := fileURI(buf.Path())
	langID := languageIDFromPath(buf.Path())
	if uri == "" || langID == "" {
		return
	}

	_ = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		a.lspMu.Lock()
		version := a.lspDocVersions[uri]
		if version == 0 {
			a.lspDocVersions[uri] = 1
			a.lspMu.Unlock()
			return client.DidOpen(uri, langID, 1, text)
		}
		version++
		a.lspDocVersions[uri] = version
		a.lspMu.Unlock()
		return client.DidChange(uri, version, text)
	})
}

func (a *maneApp) notifyLSPDidSave(buf *editor.Buffer) {
	if buf == nil || buf.Path() == "" || a.lspCtx == nil {
		return
	}
	uri := fileURI(buf.Path())
	langID := languageIDFromPath(buf.Path())
	if uri == "" || langID == "" {
		return
	}
	_ = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		return client.DidSave(uri)
	})
}

func (a *maneApp) notifyLSPDidClose(buf *editor.Buffer) {
	if buf == nil || buf.Path() == "" || a.lspCtx == nil {
		return
	}
	uri := fileURI(buf.Path())
	langID := languageIDFromPath(buf.Path())
	if uri == "" || langID == "" {
		return
	}
	a.lspMu.Lock()
	delete(a.lspDocVersions, uri)
	delete(a.lspDiagnostics, uri)
	a.lspMu.Unlock()
	_ = a.withRetryingLSPClient(langID, func(client *lsp.Client) error {
		return client.DidClose(uri)
	})
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
		a.syncMultiCursorFromTextArea()
		a.clearBlockSelection()
		a.highlight.setup(filepath.Base(buf.Path()))
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
		a.updateFoldRegions(text)
		a.openLSPDocument(buf)
		a.applyDiagnosticsForActiveBuffer()
	} else {
		a.textArea.SetText("")
		a.highlight.setup("")
		a.clearBlockSelection()
		a.foldState.SetRegions(nil)
		a.textArea.SetVisibleLines(nil)
		a.lspMu.Lock()
		a.diagnostics = nil
		a.lspMu.Unlock()
		a.syncMultiCursorFromTextArea()
		a.mergeAllHighlights()
	}
	a.syncTabBar()
	a.syncBreadcrumbs()
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
	a.notifyLSPDidSave(buf)
	a.status.Set(fmt.Sprintf("Saved %s", buf.Title()))
	a.updateStatus()
}

// cmdNewFile creates a new untitled buffer and switches to it.
func (a *maneApp) cmdNewFile() {
	a.tabs.NewUntitled()
	a.textArea.SetText("")
	a.highlight.setup("") // no language for untitled
	a.clearBlockSelection()
	a.foldState.SetRegions(nil)
	a.textArea.SetVisibleLines(nil)
	a.syncMultiCursorFromTextArea()
	a.syncTabBar()
	a.syncBreadcrumbs()
	a.updateStatus()
}

// cmdCloseTab closes the active tab and switches to the next buffer.
func (a *maneApp) cmdCloseTab() {
	if a.tabs.Count() == 0 {
		return
	}
	closingBuf := a.tabs.ActiveBuffer()
	a.notifyLSPDidClose(closingBuf)
	a.tabs.Close(a.tabs.Active())
	buf := a.tabs.ActiveBuffer()
	if buf != nil {
		text := buf.Text()
		a.textArea.SetText(text)
		a.syncMultiCursorFromTextArea()
		a.clearBlockSelection()
		a.highlight.setup(filepath.Base(buf.Path()))
		ranges := a.highlight.highlight([]byte(text))
		a.applyHighlights(text, ranges)
		a.updateFoldRegions(text)
		a.openLSPDocument(buf)
		a.applyDiagnosticsForActiveBuffer()
	} else {
		a.textArea.SetText("")
		a.highlight.setup("")
		a.clearBlockSelection()
		a.foldState.SetRegions(nil)
		a.textArea.SetVisibleLines(nil)
		a.lspMu.Lock()
		a.diagnostics = nil
		a.lspMu.Unlock()
		a.syncMultiCursorFromTextArea()
		a.mergeAllHighlights()
	}
	a.syncTabBar()
	a.syncBreadcrumbs()
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

// cmdToggleWordWrap toggles the editor word-wrap mode.
func (a *maneApp) cmdToggleWordWrap() {
	a.wordWrap = !a.wordWrap
	a.textArea.SetWordWrap(a.wordWrap)
	a.updateStatus()
}

func closestVisibleLine(visible []int, line int) int {
	if len(visible) == 0 {
		return 0
	}
	closest := visible[0]
	for _, candidate := range visible {
		if candidate > line {
			break
		}
		closest = candidate
	}
	return closest
}

func (a *maneApp) applyFoldVisibility() {
	text := a.textArea.Text()
	totalLines := editor.LineCount(text)
	if totalLines <= 0 {
		a.textArea.SetVisibleLines(nil)
		return
	}

	visible := a.foldState.VisibleLines(totalLines)
	if len(visible) == 0 || len(visible) == totalLines {
		a.textArea.SetVisibleLines(nil)
		return
	}

	a.textArea.SetVisibleLines(visible)
	col, row := a.textArea.CursorPosition()
	if a.foldState.IsLineHidden(row) {
		a.textArea.SetCursorPosition(col, closestVisibleLine(visible, row))
	}
}

func (a *maneApp) ensureLineVisible(line int) {
	if line < 0 {
		return
	}
	changed := false
	for a.foldState.IsLineHidden(line) {
		if !a.foldState.UnfoldAtLine(line) {
			break
		}
		changed = true
	}
	if changed {
		a.applyFoldVisibility()
	}
}

// cmdFoldAtCursor folds the region at the current cursor line.
func (a *maneApp) cmdFoldAtCursor() {
	_, row := a.textArea.CursorPosition()
	if a.foldState.FoldAtLine(row) {
		a.applyFoldVisibility()
		a.updateStatus()
	}
}

// cmdUnfoldAtCursor unfolds the region at the current cursor line.
func (a *maneApp) cmdUnfoldAtCursor() {
	_, row := a.textArea.CursorPosition()
	if a.foldState.UnfoldAtLine(row) {
		a.applyFoldVisibility()
		a.updateStatus()
	}
}

// cmdFoldAll folds all foldable regions.
func (a *maneApp) cmdFoldAll() {
	a.foldState.FoldAll()
	a.applyFoldVisibility()
	a.updateStatus()
}

// cmdUnfoldAll unfolds all regions.
func (a *maneApp) cmdUnfoldAll() {
	a.foldState.UnfoldAll()
	a.applyFoldVisibility()
	a.updateStatus()
}

// updateFoldRegions updates fold regions from tree-sitter when available.
func (a *maneApp) updateFoldRegions(text string) {
	a.foldState.SetRegions(a.highlight.detectFoldRegions(text))
	a.applyFoldVisibility()
}

// rehighlight runs a synchronous highlight pass and applies the results.
func (a *maneApp) rehighlight(text string) {
	ranges := a.highlight.highlight([]byte(text))
	a.applyHighlights(text, ranges)
	a.updateFoldRegions(text)
}

// cmdDeleteLine deletes the line at the current cursor position.
func (a *maneApp) cmdDeleteLine() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	col, row := a.textArea.CursorPosition()
	text := buf.Text()
	text = editor.DeleteLine(text, row)
	buf.SetText(text)
	a.textArea.SetText(text)
	// Clamp cursor row if it fell off the end.
	maxRow := editor.LineCount(text) - 1
	if row > maxRow {
		row = maxRow
	}
	a.textArea.SetCursorPosition(col, row)
	a.syncMultiCursorFromTextArea()
	a.rehighlight(text)
	a.updateStatus()
}

// cmdMoveLineUp moves the current line up by one position.
func (a *maneApp) cmdMoveLineUp() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	col, row := a.textArea.CursorPosition()
	text := buf.Text()
	newText := editor.MoveLine(text, row, -1)
	if newText == text {
		return
	}
	buf.SetText(newText)
	a.textArea.SetText(newText)
	a.textArea.SetCursorPosition(col, row-1)
	a.syncMultiCursorFromTextArea()
	a.rehighlight(newText)
	a.updateStatus()
}

// cmdMoveLineDown moves the current line down by one position.
func (a *maneApp) cmdMoveLineDown() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	col, row := a.textArea.CursorPosition()
	text := buf.Text()
	newText := editor.MoveLine(text, row, 1)
	if newText == text {
		return
	}
	buf.SetText(newText)
	a.textArea.SetText(newText)
	a.textArea.SetCursorPosition(col, row+1)
	a.syncMultiCursorFromTextArea()
	a.rehighlight(newText)
	a.updateStatus()
}

// cmdDuplicateLine duplicates the current line below the cursor.
func (a *maneApp) cmdDuplicateLine() {
	buf := a.tabs.ActiveBuffer()
	if buf == nil {
		return
	}
	col, row := a.textArea.CursorPosition()
	text := buf.Text()
	text = editor.DuplicateLine(text, row)
	buf.SetText(text)
	a.textArea.SetText(text)
	a.textArea.SetCursorPosition(col, row+1)
	a.syncMultiCursorFromTextArea()
	a.rehighlight(text)
	a.updateStatus()
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
	setMCPActiveEditor(app)
	defer setMCPActiveEditor(nil)
	app.lspCancel = cancel
	app.lspCtx = ctx
	app.cancel = func() {
		cancel()
		app.shutdownLSP()
	}
	defer app.shutdownLSP()
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

	// Set up go-to-line widget
	app.gotoLineW = newGotoLineWidget()
	app.gotoLineW.onSubmit = app.onGotoLine
	app.gotoLineW.onClose = app.onGotoLineClose

	// Set up file finder.
	app.fileFinder = newFileFinderWidget()
	app.fileFinder.onOpen = app.onFileFinder
	app.fileFinder.onClose = app.onFileFinderClose

	// Set up LSP helper palette for completion, references, and code actions.
	app.lspPalette = widgets.NewCommandPalette()

	// Set up rename overlay.
	app.renameW = newRenameWidget()
	app.renameW.onSubmit = app.cmdRenameSubmit
	app.renameW.onClose = func() {}

	// Build the command palette with editor actions.
	app.palette = widgets.NewCommandPalette(commands.AllCommands(commands.Actions{
		SaveFile:       app.cmdSaveFile,
		NewFile:        app.cmdNewFile,
		CloseTab:       app.cmdCloseTab,
		ToggleSidebar:  app.toggleSidebar,
		ToggleWordWrap: app.cmdToggleWordWrap,
		Quit:           func() { app.cancel() },
		Undo:           app.cmdUndo,
		Redo:           app.cmdRedo,
		Find:           func() { app.cmdFind() },
		Replace:        func() { app.cmdReplace() },
		GotoLine:       func() { app.cmdGotoLine() },
		DeleteLine:     app.cmdDeleteLine,
		MoveLineUp:     app.cmdMoveLineUp,
		MoveLineDown:   app.cmdMoveLineDown,
		DuplicateLine:  app.cmdDuplicateLine,
		FoldAtCursor:   app.cmdFoldAtCursor,
		UnfoldAtCursor: app.cmdUnfoldAtCursor,
		FoldAll:        app.cmdFoldAll,
		UnfoldAll:      app.cmdUnfoldAll,
		LspComplete:    app.cmdLspComplete,
		LspDefinition:  app.cmdLspDefinition,
		LspReferences:  app.cmdLspReferences,
		LspHover:       func() { app.cmdLspHoverPanel() },
		LspDiagnostics: app.cmdLspDiagnostics,
		LspRename:      func() { app.cmdLspRename() },
		LspCodeAction:  app.cmdLspCodeAction,
	})...)

	// Open files from CLI args, or create an untitled buffer if none.
	for _, f := range filesToOpen {
		_ = app.openFile(f)
	}
	if app.tabs.Count() == 0 {
		app.tabs.NewUntitled()
		app.syncTextArea()
		app.syncTabBar()
		app.syncBreadcrumbs()
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
		fluffy.Fixed(app.breadcrumbs),
		fluffy.Expanded(app.slot),
		fluffy.Fixed(statusBar),
	)

	// Global key interceptor for shortcuts that need to work regardless of focus.
	keys := &globalKeys{
		onMouse: app.handleGlobalMouse,
		onPaste: app.handleGlobalPaste,
		onKey:   app.handleGlobalKey,
	}

	// Stack: layout at bottom, palettes in the middle, global keys on top (gets events first).
	rootWidget := widgets.NewStack(layout, app.palette, app.fileFinder, app.lspPalette, app.renameW, keys)

	return fluffy.RunContext(ctx, rootWidget, opts...)
}

func (a *maneApp) handleGlobalMouse(mouse runtime.MouseMsg) runtime.HandleResult {
	if a.isBlockSelectionMode() && mouse.Button != runtime.MouseNone && mouse.Action == runtime.MousePress {
		a.clearBlockSelection()
	}
	if a.isMultiCursorMode() && mouse.Button != runtime.MouseNone && mouse.Action == runtime.MousePress {
		a.resetMultiCursor()
	}
	return runtime.Unhandled()
}

func (a *maneApp) handleGlobalPaste(paste runtime.PasteMsg) runtime.HandleResult {
	a.applyPaste(paste.Text)
	return runtime.Handled()
}

func (a *maneApp) handleGlobalKey(key runtime.KeyMsg) runtime.HandleResult {
	if key.Alt && key.Shift {
		switch key.Key {
		case terminal.KeyUp:
			a.expandBlockSelection(-1, 0)
			return runtime.Handled()
		case terminal.KeyDown:
			a.expandBlockSelection(1, 0)
			return runtime.Handled()
		case terminal.KeyLeft:
			a.expandBlockSelection(0, -1)
			return runtime.Handled()
		case terminal.KeyRight:
			a.expandBlockSelection(0, 1)
			return runtime.Handled()
		}
	}

	if a.isBlockSelectionMode() {
		switch key.Key {
		case terminal.KeyEscape:
			a.clearBlockSelection()
			a.updateStatus()
			return runtime.Handled()
		case terminal.KeyBackspace:
			a.applyBlockBackspace()
			return runtime.Handled()
		case terminal.KeyDelete:
			a.applyBlockDeleteForward()
			return runtime.Handled()
		case terminal.KeyEnter:
			a.applyBlockInsert("\n")
			return runtime.Handled()
		case terminal.KeyTab:
			a.applyBlockInsert("\t")
			return runtime.Handled()
		case terminal.KeyRune:
			if !key.Ctrl && !key.Alt && key.Rune != 0 {
				a.applyBlockInsert(string(key.Rune))
				return runtime.Handled()
			}
		}

		// Leave block mode when another command/navigation key is used.
		a.clearBlockSelection()
		a.updateStatus()
	}

	if a.isMultiCursorMode() {
		switch key.Key {
		case terminal.KeyCtrlD:
			a.addNextCursorOccurrence()
			return runtime.Handled()
		case terminal.KeyCtrlV:
			if text := a.clipboardText(); text != "" {
				a.applyPaste(text)
				return runtime.Handled()
			}
			return runtime.Unhandled()
		case terminal.KeyRune:
			if !key.Ctrl && !key.Alt && key.Rune != 0 {
				a.applyMultiCursorInsert(string(key.Rune))
				return runtime.Handled()
			}
		case terminal.KeyBackspace:
			a.applyMultiCursorDeleteBackspace()
			return runtime.Handled()
		case terminal.KeyDelete:
			a.applyMultiCursorDeleteForward()
			return runtime.Handled()
		case terminal.KeyEnter:
			a.applyMultiCursorInsert("\n")
			return runtime.Handled()
		case terminal.KeyTab:
			a.applyMultiCursorInsert("\t")
			return runtime.Handled()
		case terminal.KeyEscape:
			a.resetMultiCursor()
			return runtime.Handled()
		default:
			// Exit multi-cursor mode for all other navigation or editing
			// keys, and let the underlying widget handle movement/commands.
			a.resetMultiCursor()
		}
	}

	switch key.Key {
	case terminal.KeyCtrlD:
		a.addNextCursorOccurrence()
		return runtime.Handled()
	case terminal.KeyCtrlP:
		return a.cmdOpenFileFinder()
	case terminal.KeyRune:
		if key.Ctrl && key.Shift && (key.Rune == 'P' || key.Rune == 'p') {
			a.cmdShowPalette()
			return runtime.Handled()
		}
		if key.Ctrl && !key.Shift && (key.Rune == 'P' || key.Rune == 'p') {
			return a.cmdOpenFileFinder()
		}
		if key.Ctrl && key.Rune == 'h' {
			return a.cmdReplace()
		}
		if key.Ctrl && key.Shift && key.Rune == '{' {
			a.cmdFoldAtCursor()
			return runtime.Handled()
		}
		if key.Ctrl && key.Shift && key.Rune == '}' {
			a.cmdUnfoldAtCursor()
			return runtime.Handled()
		}
		if key.Ctrl && key.Rune == ']' {
			a.cmdGotoMatchingBracket()
			return runtime.Handled()
		}
		if key.Ctrl && key.Shift && key.Rune == 'K' {
			a.cmdDeleteLine()
			return runtime.Handled()
		}
		if key.Ctrl && key.Shift && key.Rune == 'D' {
			a.cmdDuplicateLine()
			return runtime.Handled()
		}
		if key.Ctrl && key.Rune == ' ' {
			a.cmdLspComplete()
			return runtime.Handled()
		}
		if key.Ctrl && key.Rune == '.' {
			a.cmdLspCodeAction()
			return runtime.Handled()
		}
		if key.Ctrl && key.Alt && (key.Rune == 'w' || key.Rune == 'W') {
			a.cmdToggleWordWrap()
			return runtime.Handled()
		}
	case terminal.KeyCtrlF:
		return a.cmdFind()
	case terminal.KeyCtrlG:
		return a.cmdGotoLine()
	case terminal.KeyF1:
		return a.cmdLspHoverPanel()
	case terminal.KeyF2:
		return a.cmdLspRename()
	case terminal.KeyF8:
		a.cmdLspDiagnostics()
		return runtime.Handled()
	case terminal.KeyF12:
		if key.Shift {
			a.cmdLspReferences()
		} else {
			a.cmdLspDefinition()
		}
		return runtime.Handled()
	case terminal.KeyCtrlB:
		a.toggleSidebar()
		return runtime.Handled()
	case terminal.KeyCtrlS:
		a.cmdSaveFile()
		return runtime.Handled()
	case terminal.KeyCtrlN:
		a.cmdNewFile()
		return runtime.Handled()
	case terminal.KeyCtrlW:
		a.cmdCloseTab()
		return runtime.Handled()
	case terminal.KeyCtrlQ:
		a.cancel()
		return runtime.Handled()
	case terminal.KeyUp:
		if key.Alt && !key.Shift {
			a.cmdMoveLineUp()
			return runtime.Handled()
		}
	case terminal.KeyDown:
		if key.Alt && !key.Shift {
			a.cmdMoveLineDown()
			return runtime.Handled()
		}
	case terminal.KeyPageUp:
		if key.Ctrl {
			a.prevTab()
			return runtime.Handled()
		}
	case terminal.KeyPageDown:
		if key.Ctrl {
			a.nextTab()
			return runtime.Handled()
		}
	}
	return runtime.Unhandled()
}

// detectLineEnding returns the dominant line ending string.
func detectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "CRLF"
	}
	return "LF"
}

// detectIndentMode returns the current indent style string for status reporting.
func detectIndentMode(text string) string {
	indent := editor.DetectIndentStyle(text)
	if indent == "\t" {
		return "tabs"
	}
	return "spaces(" + strconv.Itoa(len(indent)) + ")"
}

// detectGitBranch returns the current git branch for the provided path, if known.
func detectGitBranch(path string) string {
	if path == "" {
		return ""
	}

	cmd := exec.Command("git", "-C", filepath.Dir(path), "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
}
