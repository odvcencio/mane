package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
)

type finderFile struct {
	Rel string
	Abs string
}

type fileFinderWidget struct {
	*widgets.CommandPalette
	onOpen  func(path string)
	onClose func()
}

func newFileFinderWidget() *fileFinderWidget {
	f := &fileFinderWidget{
		CommandPalette: widgets.NewCommandPalette(),
	}
	f.SetOnExecute(func(cmd widgets.PaletteCommand) {
		if f.onOpen != nil {
			f.onOpen(cmd.ID)
		}
	})
	return f
}

func (f *fileFinderWidget) SetFiles(files []finderFile) {
	if f == nil || f.CommandPalette == nil {
		return
	}
	items := make([]widgets.PaletteCommand, 0, len(files))
	for _, file := range files {
		label := file.Rel
		if label == "" {
			label = file.Abs
		}
		items = append(items, widgets.PaletteCommand{
			ID:          file.Abs,
			Label:       label,
			Description: file.Abs,
			Category:    "Files",
		})
	}
	f.SetCommands(items)
}

func (f *fileFinderWidget) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if f == nil || f.CommandPalette == nil {
		return runtime.Unhandled()
	}
	if key, ok := msg.(runtime.KeyMsg); ok {
		if key.Key == terminal.KeyEscape {
			f.Hide()
			if f.onClose != nil {
				f.onClose()
			}
			return runtime.Handled()
		}
		if key.Key == terminal.KeyBackspace && f.Query() == "" {
			// Match SearchWidget and ReplaceWidget behavior for empty-query backspace.
			f.Hide()
			if f.onClose != nil {
				f.onClose()
			}
			return runtime.Handled()
		}
	}
	return f.CommandPalette.HandleMessage(msg)
}

func shouldSkipFinderDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func collectFinderFiles(root string) ([]finderFile, error) {
	clean := filepath.Clean(root)
	var out []finderFile
	err := filepath.WalkDir(clean, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipFinderDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(clean, path)
		if err != nil {
			rel = path
		}

		out = append(out, finderFile{
			Rel: filepath.ToSlash(rel),
			Abs: path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Rel) < strings.ToLower(out[j].Rel)
	})
	return out, nil
}
