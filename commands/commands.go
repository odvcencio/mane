package commands

import "github.com/odvcencio/fluffyui/widgets"

// Actions holds callbacks for all editor commands.
type Actions struct {
	SaveFile      func()
	NewFile       func()
	CloseTab      func()
	ToggleSidebar func()
	Quit          func()
}

// AllCommands returns the full command list for the palette.
func AllCommands(a Actions) []widgets.PaletteCommand {
	return []widgets.PaletteCommand{
		{ID: "file.save", Label: "Save File", Shortcut: "Ctrl+S", Category: "File", OnExecute: a.SaveFile},
		{ID: "file.new", Label: "New File", Shortcut: "Ctrl+N", Category: "File", OnExecute: a.NewFile},
		{ID: "file.close", Label: "Close Tab", Shortcut: "Ctrl+W", Category: "File", OnExecute: a.CloseTab},
		{ID: "view.sidebar", Label: "Toggle Sidebar", Shortcut: "Ctrl+B", Category: "View", OnExecute: a.ToggleSidebar},
		{ID: "app.quit", Label: "Quit", Shortcut: "Ctrl+Q", Category: "App", OnExecute: a.Quit},
	}
}
