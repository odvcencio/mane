package main

import (
	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/widgets"
)

// tabInfo holds display data for a single tab.
type tabInfo struct {
	title string
	dirty bool
}

// tabBar renders a horizontal row of tab titles. The active tab is highlighted
// and dirty buffers show a "*" indicator. Clicking a tab fires the onClick
// callback with the tab index.
type tabBar struct {
	widgets.Base
	tabs    []tabInfo
	active  int
	onClick func(index int)

	// Style fields
	normalStyle backend.Style
	activeStyle backend.Style
}

func newTabBar() *tabBar {
	return &tabBar{
		normalStyle: backend.DefaultStyle(),
		activeStyle: backend.DefaultStyle().Reverse(true),
	}
}

func (t *tabBar) Measure(constraints runtime.Constraints) runtime.Size {
	return runtime.Size{Width: constraints.MaxWidth, Height: 1}
}

func (t *tabBar) Render(ctx runtime.RenderContext) {
	bounds := ctx.Bounds
	if bounds.Width <= 0 || bounds.Height <= 0 || len(t.tabs) == 0 {
		return
	}

	buf := ctx.Buffer
	x := 0

	for i, tab := range t.tabs {
		if x >= bounds.Width {
			break
		}

		s := t.normalStyle
		if i == t.active {
			s = t.activeStyle
		}

		// Build label: " title* " or " title "
		label := " " + tab.title
		if tab.dirty {
			label += "*"
		}
		label += " "

		for _, r := range label {
			if x >= bounds.Width {
				break
			}
			buf.Set(bounds.X+x, bounds.Y, r, s)
			x++
		}

		// Separator between tabs
		if i < len(t.tabs)-1 && x < bounds.Width {
			buf.Set(bounds.X+x, bounds.Y, '│', t.normalStyle)
			x++
		}
	}

	// Fill remaining width with background
	for x < bounds.Width {
		buf.Set(bounds.X+x, bounds.Y, ' ', t.normalStyle)
		x++
	}
}

func (t *tabBar) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if mouse, ok := msg.(runtime.MouseMsg); ok {
		if mouse.Action == runtime.MousePress && mouse.Button == runtime.MouseLeft {
			idx := t.tabAtX(mouse.X)
			if idx >= 0 && idx < len(t.tabs) && t.onClick != nil {
				t.onClick(idx)
				return runtime.Handled()
			}
		}
	}
	return runtime.Unhandled()
}

// tabAtX returns the tab index at the given x coordinate, or -1.
func (t *tabBar) tabAtX(px int) int {
	bounds := t.Bounds()
	x := bounds.X

	for i, tab := range t.tabs {
		label := " " + tab.title
		if tab.dirty {
			label += "*"
		}
		label += " "

		tabWidth := len([]rune(label))
		if px >= x && px < x+tabWidth {
			return i
		}
		x += tabWidth

		// Account for separator
		if i < len(t.tabs)-1 {
			x++
		}
	}
	return -1
}

// setTabs replaces the tab data.
func (t *tabBar) setTabs(tabs []tabInfo, active int) {
	t.tabs = tabs
	t.active = active
}
