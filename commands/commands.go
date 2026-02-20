package commands

import "github.com/odvcencio/fluffyui/widgets"

// Actions holds callbacks for all editor commands.
type Actions struct {
	SaveFile      func()
	NewFile       func()
	CloseTab      func()
	ToggleSidebar func()
	Quit          func()
	Undo          func()
	Redo          func()
	Find          func()
	Replace       func()
	DeleteLine    func()
	MoveLineUp    func()
	MoveLineDown  func()
	DuplicateLine func()
}

// AllCommands returns the full command list for the palette.
func AllCommands(a Actions) []widgets.PaletteCommand {
	return []widgets.PaletteCommand{
		{ID: "file.save", Label: "Save File", Shortcut: "Ctrl+S", Category: "File", OnExecute: a.SaveFile},
		{ID: "file.new", Label: "New File", Shortcut: "Ctrl+N", Category: "File", OnExecute: a.NewFile},
		{ID: "file.close", Label: "Close Tab", Shortcut: "Ctrl+W", Category: "File", OnExecute: a.CloseTab},
		{ID: "view.sidebar", Label: "Toggle Sidebar", Shortcut: "Ctrl+B", Category: "View", OnExecute: a.ToggleSidebar},
		{ID: "app.quit", Label: "Quit", Shortcut: "Ctrl+Q", Category: "App", OnExecute: a.Quit},
		{ID: "edit.undo", Label: "Undo", Shortcut: "Ctrl+Z", Category: "Edit", OnExecute: a.Undo},
		{ID: "edit.redo", Label: "Redo", Shortcut: "Ctrl+Shift+Z", Category: "Edit", OnExecute: a.Redo},
		{ID: "edit.find", Label: "Find", Shortcut: "Ctrl+F", Category: "Edit", OnExecute: a.Find},
		{ID: "edit.replace", Label: "Replace", Shortcut: "Ctrl+H", Category: "Edit", OnExecute: a.Replace},
		{ID: "edit.deleteLine", Label: "Delete Line", Shortcut: "Ctrl+Shift+K", Category: "Edit", OnExecute: a.DeleteLine},
		{ID: "edit.moveLineUp", Label: "Move Line Up", Shortcut: "Alt+Up", Category: "Edit", OnExecute: a.MoveLineUp},
		{ID: "edit.moveLineDown", Label: "Move Line Down", Shortcut: "Alt+Down", Category: "Edit", OnExecute: a.MoveLineDown},
		{ID: "edit.duplicateLine", Label: "Duplicate Line", Shortcut: "Ctrl+Shift+D", Category: "Edit", OnExecute: a.DuplicateLine},
	}
}
