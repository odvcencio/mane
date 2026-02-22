package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	fluffymcp "github.com/odvcencio/fluffyui/agent/mcp"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/mane/lsp"
	"github.com/odvcencio/mane/mcptools"
)

var (
	mcpEditorMu sync.RWMutex
	mcpEditor   *maneApp
)

func init() {
	runtime.RegisterMCPEnabler(enableMCPWithManeExtensions)
}

func setMCPActiveEditor(app *maneApp) {
	mcpEditorMu.Lock()
	defer mcpEditorMu.Unlock()
	mcpEditor = app
}

func activeMCPEditor() *maneApp {
	mcpEditorMu.RLock()
	defer mcpEditorMu.RUnlock()
	return mcpEditor
}

func enableMCPWithManeExtensions(app *runtime.App, opts runtime.MCPOptions) (io.Closer, error) {
	if app == nil {
		return nil, fmt.Errorf("mcp server requires app")
	}

	srv, err := fluffymcp.NewServer(app, opts)
	if err != nil {
		return nil, err
	}

	editor := activeMCPEditor()
	if editor != nil {
		if err := registerMCPRegistry(app, srv, mcptools.NewRegistry(editor)); err != nil {
			return nil, err
		}
	}

	if err := srv.Start(); err != nil {
		return nil, err
	}
	return srv, nil
}

func registerMCPRegistry(app *runtime.App, srv *fluffymcp.Server, reg *mcptools.Registry) error {
	for _, tool := range reg.Tools() {
		tool := tool
		if err := srv.AddJSONTool(
			tool.Name,
			tool.Description,
			tool.InputSchema,
			func(ctx context.Context, params json.RawMessage) (any, error) {
				var out any
				callErr := app.Call(ctx, func(_ *runtime.App) error {
					result, err := tool.Handler(params)
					if err != nil {
						return err
					}
					out = result
					return nil
				})
				if callErr != nil {
					return nil, callErr
				}
				return out, nil
			},
		); err != nil {
			return err
		}
	}

	for _, resource := range reg.Resources() {
		resource := resource
		if err := srv.AddTextResourceTemplate(
			resource.URI,
			resource.Name,
			resource.Description,
			resource.MimeType,
			func(ctx context.Context, uri string) (string, error) {
				var text string
				callErr := app.Call(ctx, func(_ *runtime.App) error {
					content, err := resource.Handler(uri)
					if err != nil {
						return err
					}
					text = content
					return nil
				})
				if callErr != nil {
					return "", callErr
				}
				return text, nil
			},
		); err != nil {
			return err
		}
	}
	return nil
}

var _ mcptools.EditorAccess = (*maneApp)(nil)

func (a *maneApp) OpenFile(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	return a.openFile(path)
}

func (a *maneApp) ReadBuffer(path string) (string, error) {
	if path == "" {
		if buf := a.tabs.ActiveBuffer(); buf != nil {
			return buf.Text(), nil
		}
		return "", fmt.Errorf("no active buffer")
	}
	if buf := a.findBufferByPath(path); buf != nil {
		return buf.Text(), nil
	}
	return "", fmt.Errorf("buffer not open: %s", path)
}

func (a *maneApp) WriteBuffer(path string, text string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if a.findBufferByPath(path) == nil {
		if err := a.openFile(path); err != nil {
			return err
		}
	}

	buf := a.findBufferByPath(path)
	if buf == nil {
		return fmt.Errorf("buffer not open: %s", path)
	}

	buf.SetText(text)
	if a.tabs.ActiveBuffer() == buf {
		a.suppressChange = true
		a.textArea.SetText(text)
		a.suppressChange = false
		a.textArea.SelectNone()
		a.syncMultiCursorFromTextArea()
		a.clearBlockSelection()
		a.rehighlight(text)
		a.updateStatus()
	}
	a.scheduleLspDidChange(buf, text)
	return nil
}

func (a *maneApp) ApplyEdit(path string, startLine, startCol, endLine, endCol int, newText string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if a.findBufferByPath(path) == nil {
		if err := a.openFile(path); err != nil {
			return err
		}
	}
	buf := a.findBufferByPath(path)
	if buf == nil {
		return fmt.Errorf("buffer not open: %s", path)
	}

	text := buf.Text()
	start := lspOffsetFromPosition(text, lsp.Position{Line: startLine, Character: startCol})
	end := lspOffsetFromPosition(text, lsp.Position{Line: endLine, Character: endCol})
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

	updated := text[:start] + newText + text[end:]
	buf.SetText(updated)
	if a.tabs.ActiveBuffer() == buf {
		a.suppressChange = true
		a.textArea.SetText(updated)
		a.suppressChange = false
		a.syncMultiCursorFromTextArea()
		a.clearBlockSelection()
		a.rehighlight(updated)
		a.updateStatus()
	}
	a.scheduleLspDidChange(buf, updated)
	return nil
}

func (a *maneApp) SaveFile(path string) error {
	if path == "" {
		if buf := a.tabs.ActiveBuffer(); buf != nil {
			path = buf.Path()
		}
	}
	buf := a.findBufferByPath(path)
	if buf == nil {
		return fmt.Errorf("buffer not open: %s", path)
	}
	if buf.Untitled() {
		return fmt.Errorf("cannot save untitled buffer")
	}
	if err := buf.Save(); err != nil {
		return err
	}
	a.notifyLSPDidSave(buf)
	a.syncTabBar()
	a.updateStatus()
	return nil
}

func (a *maneApp) GoToLine(line int) error {
	if line < 1 {
		return fmt.Errorf("line must be >= 1")
	}
	if a.tabs.ActiveBuffer() == nil {
		return fmt.Errorf("no active buffer")
	}
	a.onGotoLine(line)
	return nil
}

func (a *maneApp) GetCursorPosition() (line, col int) {
	x, y := a.textArea.CursorPosition()
	return y + 1, x + 1
}

func (a *maneApp) Search(query string) []mcptools.SearchResult {
	buf := a.tabs.ActiveBuffer()
	if buf == nil || query == "" {
		return nil
	}

	text := buf.Text()
	lines := strings.Split(text, "\n")
	matches := buf.Find(query)
	results := make([]mcptools.SearchResult, 0, len(matches))
	for _, m := range matches {
		pos := lspPositionFromByteOffset(text, m.Start)
		line := pos.Line
		col := pos.Character
		lineText := ""
		if line >= 0 && line < len(lines) {
			lineText = lines[line]
		}
		matchText := ""
		if m.Start >= 0 && m.Start <= len(text) && m.End >= m.Start && m.End <= len(text) {
			matchText = text[m.Start:m.End]
		}
		results = append(results, mcptools.SearchResult{
			Line:    line + 1,
			Col:     col + 1,
			Text:    matchText,
			Context: lineText,
		})
	}
	return results
}

func (a *maneApp) SearchFiles(query string, root string) []mcptools.FileSearchResult {
	if query == "" {
		return nil
	}
	if root == "" {
		root = a.treeRoot
	}
	files, err := collectFinderFiles(root)
	if err != nil {
		return nil
	}

	results := make([]mcptools.FileSearchResult, 0, 128)
	for _, file := range files {
		data, err := os.ReadFile(file.Abs)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			col := strings.Index(line, query)
			if col < 0 {
				continue
			}
			results = append(results, mcptools.FileSearchResult{
				Path: file.Abs,
				Line: idx + 1,
				Col:  col + 1,
				Text: line,
			})
		}
	}
	return results
}

func (a *maneApp) GetDiagnostics(path string) []mcptools.DiagnosticInfo {
	if path == "" {
		path = a.ActiveFile()
	}
	if path == "" {
		return nil
	}

	uri := fileURI(path)
	a.lspMu.Lock()
	diags := append([]lsp.Diagnostic(nil), a.lspDiagnostics[uri]...)
	a.lspMu.Unlock()

	infos := make([]mcptools.DiagnosticInfo, 0, len(diags))
	for _, d := range diags {
		severity := "info"
		switch d.Severity {
		case 1:
			severity = "error"
		case 2:
			severity = "warning"
		case 3:
			severity = "info"
		case 4:
			severity = "hint"
		}
		infos = append(infos, mcptools.DiagnosticInfo{
			Path:     path,
			Line:     d.Range.Start.Line + 1,
			Col:      d.Range.Start.Character + 1,
			Severity: severity,
			Message:  d.Message,
			Source:   d.Source,
		})
	}
	return infos
}

func (a *maneApp) RunCommand(commandID string) error {
	switch strings.ToLower(strings.TrimSpace(commandID)) {
	case "save", "file.save":
		a.cmdSaveFile()
	case "new", "file.new":
		a.cmdNewFile()
	case "close", "file.close":
		a.cmdCloseTab()
	case "undo", "edit.undo":
		a.cmdUndo()
	case "redo", "edit.redo":
		a.cmdRedo()
	case "find", "edit.find":
		_ = a.cmdFind()
	case "replace", "edit.replace":
		_ = a.cmdReplace()
	case "goto", "gotoline", "edit.gotoline":
		_ = a.cmdGotoLine()
	case "fold", "edit.fold":
		a.cmdFoldAtCursor()
	case "unfold", "edit.unfold":
		a.cmdUnfoldAtCursor()
	case "foldall", "edit.foldall":
		a.cmdFoldAll()
	case "unfoldall", "edit.unfoldall":
		a.cmdUnfoldAll()
	case "toggle-sidebar", "view.sidebar":
		a.toggleSidebar()
	case "toggle-wrap", "view.wrap":
		a.cmdToggleWordWrap()
	default:
		return fmt.Errorf("unknown command: %s", commandID)
	}
	return nil
}

func parseTreeForText(path string, source []byte) (*gotreesitter.Tree, *gotreesitter.Language, error) {
	entry := grammars.DetectLanguage(filepath.Base(path))
	if entry == nil {
		return nil, nil, fmt.Errorf("no grammar for %s", path)
	}
	lang := entry.Language()
	support := grammars.EvaluateParseSupport(*entry, lang)
	if support.Backend == grammars.ParseBackendUnsupported {
		return nil, nil, fmt.Errorf("tree-sitter unsupported for %s", path)
	}

	parser := gotreesitter.NewParser(lang)
	var tree *gotreesitter.Tree
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		tree = parser.ParseWithTokenSource(source, ts)
	} else {
		tree = parser.Parse(source)
	}
	if tree == nil || tree.RootNode() == nil {
		return nil, nil, fmt.Errorf("failed to parse %s", path)
	}
	return tree, lang, nil
}

func formatNodeSExpr(node *gotreesitter.Node, lang *gotreesitter.Language, depth int) string {
	if node == nil {
		return ""
	}

	var b strings.Builder
	indent := strings.Repeat("  ", depth)
	b.WriteString("(")
	b.WriteString(node.Type(lang))
	if node.NamedChildCount() > 0 {
		for i := 0; i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			b.WriteString("\n")
			b.WriteString(indent)
			b.WriteString("  ")
			b.WriteString(formatNodeSExpr(child, lang, depth+1))
		}
		b.WriteString("\n")
		b.WriteString(indent)
	}
	b.WriteString(")")
	return b.String()
}

func (a *maneApp) sourceForPath(path string) ([]byte, error) {
	if buf := a.findBufferByPath(path); buf != nil {
		return []byte(buf.Text()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (a *maneApp) GetSyntaxTree(path string) (string, error) {
	if path == "" {
		path = a.ActiveFile()
	}
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	source, err := a.sourceForPath(path)
	if err != nil {
		return "", err
	}
	tree, lang, err := parseTreeForText(path, source)
	if err != nil {
		return "", err
	}
	return formatNodeSExpr(tree.RootNode(), lang, 0), nil
}

func symbolKindFromNodeType(nodeType string) string {
	t := strings.ToLower(nodeType)
	switch {
	case strings.Contains(t, "function"), strings.Contains(t, "method"):
		return "function"
	case strings.Contains(t, "class"):
		return "class"
	case strings.Contains(t, "interface"):
		return "interface"
	case strings.Contains(t, "struct"):
		return "struct"
	case strings.Contains(t, "enum"):
		return "enum"
	case strings.Contains(t, "type"):
		return "type"
	case strings.Contains(t, "var"), strings.Contains(t, "const"), strings.Contains(t, "field"):
		return "variable"
	default:
		return ""
	}
}

func extractSymbolName(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil || lang == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name", lang); nameNode != nil {
		return strings.TrimSpace(nameNode.Text(source))
	}
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		t := strings.ToLower(child.Type(lang))
		if strings.Contains(t, "identifier") || t == "name" {
			return strings.TrimSpace(child.Text(source))
		}
	}
	return ""
}

func (a *maneApp) GetSymbols(path string) ([]mcptools.SymbolInfo, error) {
	if path == "" {
		path = a.ActiveFile()
	}
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	source, err := a.sourceForPath(path)
	if err != nil {
		return nil, err
	}
	tree, lang, err := parseTreeForText(path, source)
	if err != nil {
		return nil, err
	}

	symbols := make([]mcptools.SymbolInfo, 0, 64)
	seen := make(map[string]struct{})
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		nodeType := node.Type(lang)
		kind := symbolKindFromNodeType(nodeType)
		if kind != "" {
			name := extractSymbolName(node, lang, source)
			if name != "" {
				start := int(node.StartPoint().Row) + 1
				end := int(node.EndPoint().Row) + 1
				key := fmt.Sprintf("%s:%s:%d:%d", kind, name, start, end)
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					symbols = append(symbols, mcptools.SymbolInfo{
						Name:      name,
						Kind:      kind,
						StartLine: start,
						EndLine:   end,
					})
				}
			}
		}
		for i := 0; i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(tree.RootNode())

	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].StartLine == symbols[j].StartLine {
			return symbols[i].Name < symbols[j].Name
		}
		return symbols[i].StartLine < symbols[j].StartLine
	})
	return symbols, nil
}

func (a *maneApp) ActiveFile() string {
	if buf := a.tabs.ActiveBuffer(); buf != nil {
		return buf.Path()
	}
	return ""
}

func (a *maneApp) ListOpenFiles() []string {
	files := make([]string, 0, a.tabs.Count())
	for _, buf := range a.tabs.Buffers() {
		if buf == nil || buf.Path() == "" {
			continue
		}
		files = append(files, buf.Path())
	}
	sort.Strings(files)
	return files
}

func (a *maneApp) ProjectRoot() string {
	return a.treeRoot
}
