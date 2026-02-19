package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/fluffyui/fluffy"
	"github.com/odvcencio/fluffyui/state"
	"github.com/odvcencio/fluffyui/widgets"

	"github.com/odvcencio/mane/editor"
)

// maneApp holds the core state for the editor application.
type maneApp struct {
	tabs     *editor.TabManager
	textArea *widgets.TextArea
	fileTree *widgets.DirectoryTree
	status   *state.Signal[string]
}

// newManeApp creates a maneApp with the given root directory for the file tree.
func newManeApp(treeRoot string) *maneApp {
	app := &maneApp{
		tabs:     editor.NewTabManager(),
		textArea: widgets.NewTextArea(),
		status:   state.NewSignal[string](" untitled"),
	}

	app.textArea.SetLabel("Editor")

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
	})

	return app
}

// openFile opens a file by path through the TabManager and loads its content
// into the TextArea.
func (a *maneApp) openFile(path string) {
	_, err := a.tabs.OpenFile(path)
	if err != nil {
		a.status.Set(fmt.Sprintf(" error: %v", err))
		return
	}
	a.syncTextArea()
	a.updateStatus()
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
// Format: " {title}{dirty}  Ln {row+1}, Col {col+1}"
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

	col, row := a.textArea.CursorPosition()
	a.status.Set(fmt.Sprintf(" %s%s  Ln %d, Col %d", buf.Title(), dirty, row+1, col+1))
}

// run constructs the editor layout and starts the FluffyUI app.
func run(ctx context.Context, root, theme string, opts ...fluffy.AppOption) error {
	_ = theme // reserved for future theme loading

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

	return fluffy.RunContext(ctx, layout, opts...)
}
