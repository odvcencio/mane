package main

import (
	"strings"
	"unicode/utf8"

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
)

type renameWidget struct {
	widgets.Base

	query   string
	focused bool

	onSubmit func(name string)
	onClose  func()

	bgStyle      backend.Style
	labelStyle   backend.Style
	inputStyle   backend.Style
	counterStyle backend.Style
}

func newRenameWidget() *renameWidget {
	return &renameWidget{
		bgStyle:      backend.DefaultStyle(),
		labelStyle:   backend.DefaultStyle().Foreground(backend.ColorRGB(0x88, 0x88, 0x88)),
		inputStyle:   backend.DefaultStyle(),
		counterStyle: backend.DefaultStyle().Foreground(backend.ColorYellow),
	}
}

func (w *renameWidget) Focus() {
	w.focused = true
}

func (w *renameWidget) Blur() {
	w.focused = false
}

func (w *renameWidget) SetText(text string) {
	w.query = text
}

func (w *renameWidget) Query() string {
	return w.query
}

// Measure returns the preferred size for the overlay.
func (w *renameWidget) Measure(constraints runtime.Constraints) runtime.Size {
	return runtime.Size{Width: constraints.MaxWidth, Height: 1}
}

// Layout positions the widget at the bottom of the screen.
func (w *renameWidget) Layout(bounds runtime.Rect) {
	height := 1
	if bounds.Height < height {
		height = bounds.Height
	}
	w.Base.Layout(runtime.Rect{
		X:      bounds.X,
		Y:      bounds.Y + bounds.Height - height,
		Width:  bounds.Width,
		Height: height,
	})
}

// Render draws the prompt and the currently typed symbol.
func (w *renameWidget) Render(ctx runtime.RenderContext) {
	if w == nil {
		return
	}
	b := w.Bounds()
	if b.Width <= 0 || b.Height < 1 {
		return
	}

	ctx.Buffer.Fill(b, ' ', w.bgStyle)

	prefix := "Rename symbol: "
	x := b.X
	ctx.Buffer.SetString(x, b.Y, prefix, w.labelStyle)
	x += len(prefix)

	maxValue := b.Width - len(prefix) - 2
	if maxValue < 1 {
		maxValue = 1
	}
	text := w.query
	if utf8.RuneCountInString(text) > maxValue {
		runes := []rune(text)
		text = string(runes[len(runes)-maxValue:])
	}
	ctx.Buffer.SetString(x, b.Y, text, w.inputStyle)

	cursorX := x + utf8.RuneCountInString(text)
	if w.focused && cursorX < b.X+b.Width {
		ctx.Buffer.Set(cursorX, b.Y, '█', w.inputStyle)
	}

	if w.query == "" {
		msg := "Enter new symbol name"
		available := b.Width - len(prefix) - 1
		if available <= 0 {
			return
		}
		if len(msg) > available {
			msg = msg[:available]
		}
		if msg != "" {
			ctx.Buffer.SetString(b.X+b.Width-len(msg), b.Y, msg, w.counterStyle)
		}
	}
}

// HandleMessage handles input while the widget has focus.
func (w *renameWidget) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if w == nil || !w.focused {
		return runtime.Unhandled()
	}
	key, ok := msg.(runtime.KeyMsg)
	if !ok {
		return runtime.Unhandled()
	}

	switch key.Key {
	case terminal.KeyEscape:
		if w.onClose != nil {
			w.onClose()
		}
		return runtime.WithCommand(runtime.PopOverlay{})
	case terminal.KeyEnter:
		name := strings.TrimSpace(w.query)
		if w.onSubmit != nil {
			w.onSubmit(name)
		}
		return runtime.WithCommand(runtime.PopOverlay{})
	case terminal.KeyBackspace:
		if len(w.query) > 0 {
			_, size := utf8.DecodeLastRuneInString(w.query)
			if size > 0 {
				w.query = w.query[:len(w.query)-size]
			}
		}
		return runtime.Handled()
	case terminal.KeyRune:
		if key.Ctrl {
			return runtime.Unhandled()
		}
		w.query += string(key.Rune)
		return runtime.Handled()
	}
	return runtime.Unhandled()
}
