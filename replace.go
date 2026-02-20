package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/odvcencio/fluffyui/backend"
	"github.com/odvcencio/fluffyui/runtime"
	"github.com/odvcencio/fluffyui/terminal"
	"github.com/odvcencio/fluffyui/widgets"
)

// replaceField identifies which text field has focus in the replace widget.
type replaceField int

const (
	fieldSearch  replaceField = iota
	fieldReplace
)

// replaceWidget renders a two-row overlay at the bottom of the screen:
//
//	Row 1: "Find: " + search input + match counter (e.g., "1/3")
//	Row 2: "Replace: " + replace input + [Replace] [All] buttons
//
// Keyboard handling:
//   - Tab switches focus between search and replace fields
//   - Arrow Up/Down navigates between search matches
//   - Enter replaces the current match
//   - Ctrl+Enter replaces all matches
//   - Escape closes the widget
//   - Backspace deletes the last character in the focused field
type replaceWidget struct {
	widgets.Base

	searchText  string
	replaceText string
	activeField replaceField
	focused     bool

	matchCount   int
	currentMatch int

	// Callbacks
	onSearch     func(query string)
	onNext       func()
	onPrev       func()
	onReplace    func(search, replace string)
	onReplaceAll func(search, replace string)
	onClose      func()

	// Styles
	bgStyle     backend.Style
	labelStyle  backend.Style
	inputStyle  backend.Style
	buttonStyle backend.Style
	matchStyle  backend.Style
}

func newReplaceWidget() *replaceWidget {
	return &replaceWidget{
		bgStyle:     backend.DefaultStyle(),
		labelStyle:  backend.DefaultStyle().Foreground(backend.ColorRGB(0x88, 0x88, 0x88)),
		inputStyle:  backend.DefaultStyle(),
		buttonStyle: backend.DefaultStyle().Reverse(true),
		matchStyle:  backend.DefaultStyle().Foreground(backend.ColorYellow),
	}
}

// Focus marks the widget as focused and resets focus to the search field.
func (w *replaceWidget) Focus() {
	w.focused = true
	w.activeField = fieldSearch
}

// Blur marks the widget as unfocused.
func (w *replaceWidget) Blur() {
	w.focused = false
}

// SetMatchInfo updates the match count display.
func (w *replaceWidget) SetMatchInfo(current, total int) {
	w.currentMatch = current
	w.matchCount = total
}

// Measure returns the preferred size: full width, 2 rows tall.
func (w *replaceWidget) Measure(constraints runtime.Constraints) runtime.Size {
	return runtime.Size{Width: constraints.MaxWidth, Height: 2}
}

// Layout positions the widget at the bottom of the available space.
func (w *replaceWidget) Layout(bounds runtime.Rect) {
	height := 2
	if height > bounds.Height {
		height = bounds.Height
	}
	w.Base.Layout(runtime.Rect{
		X:      bounds.X,
		Y:      bounds.Y + bounds.Height - height,
		Width:  bounds.Width,
		Height: height,
	})
}

// Render draws both rows of the replace widget.
func (w *replaceWidget) Render(ctx runtime.RenderContext) {
	b := w.Bounds()
	buf := ctx.Buffer
	if b.Width <= 0 || b.Height < 2 {
		return
	}

	// Fill background for both rows.
	buf.Fill(b, ' ', w.bgStyle)

	// ---- Row 1: "Find: " + search input + match counter ----
	findLabel := "Find: "
	y0 := b.Y
	x := b.X

	buf.SetString(x, y0, findLabel, w.labelStyle)
	x += len(findLabel)

	// Determine max query display width (leave room for match counter).
	counterReserve := 12 // enough for "999/999" + padding
	maxQueryW := b.Width - len(findLabel) - counterReserve
	if maxQueryW < 1 {
		maxQueryW = 1
	}

	query := w.searchText
	queryRuneCount := utf8.RuneCountInString(query)
	if queryRuneCount > maxQueryW {
		// Truncate from the left to show the end of the query.
		runes := []rune(query)
		query = string(runes[queryRuneCount-maxQueryW:])
	}
	buf.SetString(x, y0, query, w.inputStyle)
	cursorX := x + utf8.RuneCountInString(query)

	// Draw cursor if this field is active.
	if w.focused && w.activeField == fieldSearch && cursorX < b.X+b.Width-counterReserve {
		buf.Set(cursorX, y0, '\u2588', w.inputStyle)
	}

	// Match counter on the right side.
	var counter string
	if w.matchCount > 0 {
		counter = fmt.Sprintf("%d/%d", w.currentMatch+1, w.matchCount)
	} else if w.searchText != "" {
		counter = "No matches"
	}
	if counter != "" {
		counterX := b.X + b.Width - utf8.RuneCountInString(counter) - 1
		buf.SetString(counterX, y0, counter, w.matchStyle)
	}

	// ---- Row 2: "Replace: " + replace input + [Replace] [All] buttons ----
	replaceLabel := "Replace: "
	y1 := b.Y + 1
	x = b.X

	buf.SetString(x, y1, replaceLabel, w.labelStyle)
	x += len(replaceLabel)

	// Buttons drawn on the right; reserve space.
	btnReplace := " Replace "
	btnAll := " All "
	btnSpace := 1 // gap between buttons
	btnTotalW := len(btnReplace) + btnSpace + len(btnAll) + 1

	maxReplaceW := b.Width - len(replaceLabel) - btnTotalW
	if maxReplaceW < 1 {
		maxReplaceW = 1
	}

	rtext := w.replaceText
	rtextRuneCount := utf8.RuneCountInString(rtext)
	if rtextRuneCount > maxReplaceW {
		runes := []rune(rtext)
		rtext = string(runes[rtextRuneCount-maxReplaceW:])
	}
	buf.SetString(x, y1, rtext, w.inputStyle)
	replaceCursorX := x + utf8.RuneCountInString(rtext)

	// Draw cursor if this field is active.
	if w.focused && w.activeField == fieldReplace && replaceCursorX < b.X+b.Width-btnTotalW {
		buf.Set(replaceCursorX, y1, '\u2588', w.inputStyle)
	}

	// Draw buttons on the right.
	btnX := b.X + b.Width - btnTotalW
	buf.SetString(btnX, y1, btnReplace, w.buttonStyle)
	btnX += len(btnReplace) + btnSpace
	buf.SetString(btnX, y1, btnAll, w.buttonStyle)
}

// HandleMessage processes keyboard and mouse input.
func (w *replaceWidget) HandleMessage(msg runtime.Message) runtime.HandleResult {
	if !w.focused {
		return runtime.Unhandled()
	}

	switch m := msg.(type) {
	case runtime.KeyMsg:
		return w.handleKey(m)
	case runtime.MouseMsg:
		return w.handleMouse(m)
	}

	return runtime.Unhandled()
}

// handleKey processes keyboard events for the replace widget.
func (w *replaceWidget) handleKey(key runtime.KeyMsg) runtime.HandleResult {
	switch key.Key {
	case terminal.KeyEscape:
		if w.onClose != nil {
			w.onClose()
		}
		return runtime.WithCommand(runtime.PopOverlay{})

	case terminal.KeyTab:
		// Toggle between search and replace fields.
		if w.activeField == fieldSearch {
			w.activeField = fieldReplace
		} else {
			w.activeField = fieldSearch
		}
		return runtime.Handled()

	case terminal.KeyUp:
		if w.onPrev != nil {
			w.onPrev()
		}
		return runtime.Handled()

	case terminal.KeyDown:
		if w.onNext != nil {
			w.onNext()
		}
		return runtime.Handled()

	case terminal.KeyEnter:
		if key.Ctrl {
			// Ctrl+Enter: replace all.
			if w.onReplaceAll != nil {
				w.onReplaceAll(w.searchText, w.replaceText)
			}
		} else {
			// Enter: replace current match.
			if w.onReplace != nil {
				w.onReplace(w.searchText, w.replaceText)
			}
		}
		return runtime.Handled()

	case terminal.KeyBackspace:
		w.deleteChar()
		return runtime.Handled()

	case terminal.KeyRune:
		if key.Ctrl {
			// Ignore ctrl+letter combos (except let them bubble up).
			return runtime.Unhandled()
		}
		w.insertChar(key.Rune)
		return runtime.Handled()
	}

	return runtime.Unhandled()
}

// handleMouse processes mouse clicks on the [Replace] and [All] buttons.
func (w *replaceWidget) handleMouse(mouse runtime.MouseMsg) runtime.HandleResult {
	if mouse.Action != runtime.MousePress || mouse.Button != runtime.MouseLeft {
		return runtime.Unhandled()
	}

	b := w.Bounds()
	if b.Width <= 0 || b.Height < 2 {
		return runtime.Unhandled()
	}

	// Check if click is on row 2 (buttons row).
	y1 := b.Y + 1
	if mouse.Y != y1 {
		return runtime.Unhandled()
	}

	// Calculate button positions (same as in Render).
	btnReplace := " Replace "
	btnAll := " All "
	btnSpace := 1
	btnTotalW := len(btnReplace) + btnSpace + len(btnAll) + 1

	btnReplaceX := b.X + b.Width - btnTotalW
	btnAllX := btnReplaceX + len(btnReplace) + btnSpace

	if mouse.X >= btnReplaceX && mouse.X < btnReplaceX+len(btnReplace) {
		if w.onReplace != nil {
			w.onReplace(w.searchText, w.replaceText)
		}
		return runtime.Handled()
	}

	if mouse.X >= btnAllX && mouse.X < btnAllX+len(btnAll) {
		if w.onReplaceAll != nil {
			w.onReplaceAll(w.searchText, w.replaceText)
		}
		return runtime.Handled()
	}

	return runtime.Unhandled()
}

// insertChar appends a rune to the currently active field.
func (w *replaceWidget) insertChar(r rune) {
	if w.activeField == fieldSearch {
		w.searchText += string(r)
		if w.onSearch != nil {
			w.onSearch(w.searchText)
		}
	} else {
		w.replaceText += string(r)
	}
}

// deleteChar removes the last rune from the currently active field.
func (w *replaceWidget) deleteChar() {
	if w.activeField == fieldSearch {
		if len(w.searchText) > 0 {
			_, size := utf8.DecodeLastRuneInString(w.searchText)
			w.searchText = w.searchText[:len(w.searchText)-size]
			if w.onSearch != nil {
				w.onSearch(w.searchText)
			}
		}
	} else {
		if len(w.replaceText) > 0 {
			_, size := utf8.DecodeLastRuneInString(w.replaceText)
			w.replaceText = w.replaceText[:len(w.replaceText)-size]
		}
	}
}
