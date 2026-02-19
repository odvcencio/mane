package main

import (
	"context"
	"path/filepath"

	"github.com/odvcencio/fluffyui/fluffy"
	"github.com/odvcencio/fluffyui/widgets"
)

// run constructs the editor layout and starts the FluffyUI app.
func run(ctx context.Context, root, theme string, opts ...fluffy.AppOption) error {
	_ = theme // reserved for future theme loading

	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	// Status bar at the bottom.
	status := fluffy.Label("Ready")

	// Editor pane.
	editor := widgets.NewTextArea()
	editor.SetLabel("Editor")

	// Directory tree on the left.
	tree := widgets.NewDirectoryTree(absRoot,
		widgets.WithLazyLoad(true),
		widgets.WithOnSelect(func(path string) {
			status.SetText(path)
		}),
	)

	// Horizontal split: tree (25%) | editor (75%).
	splitter := widgets.NewSplitter(tree, editor)
	splitter.Ratio = 0.25

	// Vertical layout: splitter fills space, status bar fixed at bottom.
	layout := fluffy.VFlex(
		fluffy.Expanded(splitter),
		fluffy.Fixed(status),
	)

	return fluffy.RunContext(ctx, layout, opts...)
}
