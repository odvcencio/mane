package main

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
)

type gotoLineWidget struct {
	widgets.Base

	query    string
	focused  bool
	onSubmit func(line int)
	onClose  func()

	bgStyle      backend.Style
	labelStyle   backend.Style
	inputStyle   backend.Style
	counterStyle backend.Style
}

func newGotoLineWidget() *gotoLineWidget {
	return &gotoLineWidget{
		bgStyle:      backend.DefaultStyle(),
		labelStyle:   backend.DefaultStyle().Foreground(backend.ColorRGB(0x88, 0x88, 0x88)),
		inputStyle:   backend.DefaultStyle(),
		counterStyle: backend.DefaultStyle().Foreground(backend.ColorYellow),
	}
}

func (w *gotoLineWidget) Focus() {
	w.focused = true
}

func (w *gotoLineWidget) Blur() {
	w.focused = false
}

func (w *gotoLineWidget) SetQuery(query string) {
	w.query = query
}

// Measure returns the preferred size for the overlay.
func (w *gotoLineWidget) Measure(constraints runtime.Constraints) runtime.Size {
	return runtime.Size{Width: constraints.MaxWidth, Height: 1}
}

// Layout positions the widget at the bottom of the screen.
func (w *gotoLineWidget) Layout(bounds runtime.Rect) {
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

// Render draws the prompt and the currently typed line number.
func (w *gotoLineWidget) Render(ctx runtime.RenderContext) {
	if w == nil {
		return
	}
	b := w.Bounds()
	if b.Width <= 0 || b.Height < 1 {
		return
	}

	ctx.Buffer.Fill(b, ' ', w.bgStyle)

	prefix := "Go to line: "
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
		msg := "Enter line number"
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
func (w *gotoLineWidget) HandleMessage(msg runtime.Message) runtime.HandleResult {
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
		line := 0
		if trimmed := strings.TrimSpace(w.query); trimmed != "" {
			n, err := strconv.Atoi(trimmed)
			if err == nil {
				line = n
			}
		}
		if w.onSubmit != nil {
			w.onSubmit(line)
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
		if key.Rune >= '0' && key.Rune <= '9' {
			w.query += string(key.Rune)
		}
		return runtime.Handled()
	}
	return runtime.Unhandled()
}
