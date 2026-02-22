package commands

import "github.com/odvcencio/fluffyui/widgets"

// Actions holds callbacks for all editor commands.
type Actions struct {
	SaveFile       func()
	NewFile        func()
	CloseTab       func()
	ToggleSidebar  func()
	ToggleWordWrap func()
	Quit           func()
	Undo           func()
	Redo           func()
	Find           func()
	Replace        func()
	GotoLine       func()
	DeleteLine     func()
	MoveLineUp     func()
	MoveLineDown   func()
	DuplicateLine  func()
	// Folding actions.
	FoldAtCursor   func()
	UnfoldAtCursor func()
	FoldAll        func()
	UnfoldAll      func()
	// LSP actions.
	LspComplete    func()
	LspDefinition  func()
	LspReferences  func()
	LspHover       func()
	LspDiagnostics func()
	LspRename      func()
	LspCodeAction  func()
}

// AllCommands returns the full command list for the palette.
func AllCommands(a Actions) []widgets.PaletteCommand {
	return []widgets.PaletteCommand{
		{ID: "file.save", Label: "Save File", Shortcut: "Ctrl+S", Category: "File", OnExecute: a.SaveFile},
		{ID: "file.new", Label: "New File", Shortcut: "Ctrl+N", Category: "File", OnExecute: a.NewFile},
		{ID: "file.close", Label: "Close Tab", Shortcut: "Ctrl+W", Category: "File", OnExecute: a.CloseTab},
		{ID: "view.sidebar", Label: "Toggle Sidebar", Shortcut: "Ctrl+B", Category: "View", OnExecute: a.ToggleSidebar},
		{ID: "view.wrap", Label: "Toggle Word Wrap", Shortcut: "Ctrl+Alt+W", Category: "View", OnExecute: a.ToggleWordWrap},
		{ID: "app.quit", Label: "Quit", Shortcut: "Ctrl+Q", Category: "App", OnExecute: a.Quit},
		{ID: "edit.undo", Label: "Undo", Shortcut: "Ctrl+Z", Category: "Edit", OnExecute: a.Undo},
		{ID: "edit.redo", Label: "Redo", Shortcut: "Ctrl+Shift+Z", Category: "Edit", OnExecute: a.Redo},
		{ID: "edit.find", Label: "Find", Shortcut: "Ctrl+F", Category: "Edit", OnExecute: a.Find},
		{ID: "edit.replace", Label: "Replace", Shortcut: "Ctrl+H", Category: "Edit", OnExecute: a.Replace},
		{ID: "edit.gotoLine", Label: "Go To Line", Shortcut: "Ctrl+G", Category: "Navigation", OnExecute: a.GotoLine},
		{ID: "edit.deleteLine", Label: "Delete Line", Shortcut: "Ctrl+Shift+K", Category: "Edit", OnExecute: a.DeleteLine},
		{ID: "edit.moveLineUp", Label: "Move Line Up", Shortcut: "Alt+Up", Category: "Edit", OnExecute: a.MoveLineUp},
		{ID: "edit.moveLineDown", Label: "Move Line Down", Shortcut: "Alt+Down", Category: "Edit", OnExecute: a.MoveLineDown},
		{ID: "edit.duplicateLine", Label: "Duplicate Line", Shortcut: "Ctrl+Shift+D", Category: "Edit", OnExecute: a.DuplicateLine},
		{ID: "edit.fold", Label: "Fold", Shortcut: "Ctrl+Shift+[", Category: "Edit", OnExecute: a.FoldAtCursor},
		{ID: "edit.unfold", Label: "Unfold", Shortcut: "Ctrl+Shift+]", Category: "Edit", OnExecute: a.UnfoldAtCursor},
		{ID: "edit.foldAll", Label: "Fold All", Category: "Edit", OnExecute: a.FoldAll},
		{ID: "edit.unfoldAll", Label: "Unfold All", Category: "Edit", OnExecute: a.UnfoldAll},
		{ID: "lsp.complete", Label: "LSP Completion", Shortcut: "Ctrl+Space", Category: "Language", OnExecute: a.LspComplete},
		{ID: "lsp.definition", Label: "Go to Definition", Shortcut: "F12", Category: "Language", OnExecute: a.LspDefinition},
		{ID: "lsp.references", Label: "Find References", Shortcut: "Shift+F12", Category: "Language", OnExecute: a.LspReferences},
		{ID: "lsp.hover", Label: "Show Hover", Shortcut: "F1", Category: "Language", OnExecute: a.LspHover},
		{ID: "lsp.diagnostics", Label: "Show Diagnostics", Shortcut: "F8", Category: "Language", OnExecute: a.LspDiagnostics},
		{ID: "lsp.rename", Label: "Rename Symbol", Shortcut: "F2", Category: "Language", OnExecute: a.LspRename},
		{ID: "lsp.codeAction", Label: "Code Actions", Shortcut: "Ctrl+.", Category: "Language", OnExecute: a.LspCodeAction},
	}
}
