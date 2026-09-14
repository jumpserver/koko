package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/jumpserver/koko/internal/tui"
	"github.com/rivo/tview"
)

type tuiAccountSearch struct {
	picker  *tview.DropDown
	field   *tview.InputField
	matches []int
}

func (s *tuiAccountSearch) handleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyRune, tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete,
		tcell.KeyLeft, tcell.KeyRight, tcell.KeyCtrlW:
		s.field.InputHandler()(ev, func(tview.Primitive) {})
		return true
	case tcell.KeyCtrlU:
		s.field.SetText("")
		return true
	}
	return false
}

type tuiNavigationSearch struct {
	picker *tview.DropDown
	field  *tview.InputField
}

// DropDown's private prefix field handles text without drawing a cursor. Use
// one visible input field instead, so local IME composition has a caret anchor.
func (h *terminalUI) navigationSearchFor(d *tview.DropDown) *tview.InputField {
	if d == nil || d != h.org && d != h.treeKind {
		return nil
	}
	if h.navigationSearch == nil || h.navigationSearch.picker != d {
		field := tview.NewInputField().SetFieldTextColor(tui.Foreground).
			SetFieldBackgroundColor(tui.Panel).SetPlaceholderTextColor(tui.Foreground)
		field.SetBackgroundColor(tui.Panel)
		field.SetChangedFunc(func(prefix string) {
			if prefix == "" || !d.IsOpen() {
				return
			}
			d.Focus(func(p tview.Primitive) {
				list := p.(*tview.List)
				for index := range list.GetItemCount() {
					text, _ := list.GetItemText(index)
					// Navigation labels are escaped; each list item has one
					// padding space on either side from SetTextOptions.
					text = tview.Unescape(strings.TrimSuffix(strings.TrimPrefix(text, " "), " "))
					if strings.HasPrefix(strings.ToLower(text), strings.ToLower(prefix)) {
						list.SetCurrentItem(index)
						break
					}
				}
			})
		})
		h.navigationSearch = &tuiNavigationSearch{picker: d, field: field}
	}
	field := h.navigationSearch.field
	if !d.IsOpen() {
		field.SetText("")
	}
	return field
}

func (h *terminalUI) drawNavigationSearch(screen tcell.Screen) {
	d := h.focusedDropdown()
	field := h.navigationSearchFor(d)
	if field == nil {
		h.navigationSearch = nil
		return
	}
	x, y, width, height := d.GetInnerRect()
	labelWidth := tview.TaggedStringWidth(d.GetLabel())
	width = min(width-labelWidth, d.GetFieldWidth())
	if width < 1 || height < 1 {
		return
	}
	_, selected := d.GetCurrentOption()
	field.SetPlaceholder(tview.Unescape(selected)).SetRect(x+labelWidth, y, width, 1)
	field.Focus(nil)
	field.Draw(screen)
	field.Blur()
	screen.SetCursorStyle(tcell.CursorStyleBlinkingBar)
}

func (h *terminalUI) pasteNavigationSearch(text string, setFocus func(tview.Primitive)) bool {
	field := h.navigationSearchFor(h.focusedDropdown())
	if field == nil {
		return false
	}
	h.openFocusedDropdown()
	field.PasteHandler()(text, setFocus)
	return true
}

// Keep a dialog dropdown arrow immediately inside its right border, outside
// the native text area. The dialog fields have a border and one left pad cell.
func pinDialogDropdownIndicator(d *tview.DropDown) {
	drawBorder := d.GetDrawFunc()
	d.SetDrawFunc(func(s tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if drawBorder != nil {
			drawBorder(s, x, y, width, height)
		}
		innerX, innerY := x+2, y+1
		innerWidth, innerHeight := max(0, width-4), max(0, height-2)
		if innerWidth < 3 || innerHeight < 1 {
			return innerX, innerY, innerWidth, innerHeight
		}
		s.SetContent(x+width-3, innerY, '▾', nil, tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel))
		return innerX, innerY, innerWidth - 2, innerHeight
	})
}

func pinNavigationDropdownIndicator(d *tview.DropDown) {
	d.SetTextOptions(" ", " ", "", "", " … ")
	d.SetDrawFunc(func(s tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if width < 3 || height < 1 {
			return x, y, width, height
		}
		s.SetContent(x+width-1, y, '▾', nil, tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel))
		return x, y, width - 2, height
	})
	d.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y, width, _ := d.GetRect()
		mx, my := ev.Position()
		if !d.IsOpen() && width >= 3 && my == y && mx >= x+width-2 && mx < x+width {
			// The reserved arrow is outside GetInnerRect. Route its click to
			// the same native field handler, preserving focus and list routing.
			ev = tcell.NewEventMouse(x, y, ev.Buttons(), ev.Modifiers())
		}
		return action, ev
	})
}

func (h *terminalUI) accountSearchFor(d *tview.DropDown) *tuiAccountSearch {
	if h.modal {
		search := h.dialogs[len(h.dialogs)-1].accountSearch
		if search != nil && search.picker == d {
			return search
		}
	}
	return nil
}

func (h *terminalUI) focusedDropdown() *tview.DropDown {
	for _, item := range h.focusOrder() {
		if d, ok := item.(*tview.DropDown); ok && d.HasFocus() {
			return d
		}
	}
	return nil
}

func (h *terminalUI) openFocusedDropdown() {
	if d := h.focusedDropdown(); d != nil && !d.IsOpen() && d.GetOptionCount() > 0 {
		d.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { h.app.SetFocus(p) })
		if h.modal && h.dialogs[len(h.dialogs)-1].boundedDropdowns {
			d.Focus(func(p tview.Primitive) { h.limitDialogDropdown(d, p.(*tview.List)) })
		}
	}
}

const tuiDropdownVisibleItems = 8

// Bound the native list before it paints or adjusts its scroll offset. Drawing
// a second, smaller list afterward leaves oversized remnants and resets scroll.
func (h *terminalUI) limitDialogDropdown(d *tview.DropDown, list *tview.List) {
	digits, width, labelWidth := len(strconv.Itoa(list.GetItemCount())), 0, 0
	for i := range list.GetItemCount() {
		label, _ := list.GetItemText(i)
		width = max(width, tview.TaggedStringWidth(label)+digits+6)
	}
	for _, item := range h.focusOrder() {
		if picker, ok := item.(*tview.DropDown); ok {
			labelWidth = max(labelWidth, tview.TaggedStringWidth(picker.GetLabel()))
		}
	}
	frame := tview.NewBox().SetBorderPadding(0, 0, digits+3, 2)
	tuiBorder(frame, "", tui.FocusBorder)
	// Flex supplies a transparent Box, so the native, unbounded rectangle is
	// never cleared. Keep the native input capture and delegated list focus.
	capture, focused := list.GetInputCapture(), list.HasFocus()
	list.Box = tview.NewFlex().Box
	list.SetInputCapture(capture)
	search := h.accountSearchFor(d)
	topPadding := 0
	if search != nil {
		topPadding = 2 // Search row and separator, followed immediately by options.
		list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if search.handleKey(ev) {
				return nil
			}
			if ev.Key() == tcell.KeyEnter && len(search.matches) == 0 {
				return nil
			}
			return capture(ev)
		})
	}
	if focused {
		list.Focus(nil)
	}
	list.SetDrawFunc(func(screen tcell.Screen, _, _, _, _ int) (int, int, int, int) {
		sw, sh := screen.Size()
		dx, dy, dw, _ := d.GetInnerRect()
		w := min(max(width, dw-labelWidth), max(1, sw-2))
		x := max(0, min(dx+labelWidth, sw-w-1))
		below, above := max(0, sh-dy-2), max(0, dy-1)
		height := min(min(list.GetItemCount(), tuiDropdownVisibleItems)+topPadding+2, max(below, above))
		y := dy + 1
		if below < height {
			y = dy - height
		}
		list.SetRect(x, y, w, height)
		digits := len(strconv.Itoa(list.GetItemCount()))
		frame.SetBorderPadding(topPadding, 0, digits+3, 2)
		frame.SetRect(x, y, w, height)
		frame.SetBackgroundColor(tui.Panel).SetBorderColor(tui.FocusBorder)
		frame.Draw(screen)
		if search != nil {
			separator := tcell.StyleDefault.Foreground(tui.Border).Background(tui.Panel)
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, y+2, '─', nil, separator)
			}
			corners := separator.Foreground(tui.FocusBorder)
			screen.SetContent(x, y+2, '├', nil, corners)
			screen.SetContent(x+w-1, y+2, '┤', nil, corners)
			search.field.SetRect(x+2, y+1, max(0, w-4), 1)
			search.field.Focus(nil)
			search.field.Draw(screen)
			search.field.Blur()
			screen.SetCursorStyle(tcell.CursorStyleBlinkingBar)
			list.SetSelectedStyle(tui.Selected)
			if len(search.matches) == 0 {
				list.SetSelectedStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel))
			}
		}
		_, row, _, visible := frame.GetInnerRect()
		if visible == 0 {
			return x, y, 0, 0
		}
		offset, horizontal := list.GetOffset()
		current, count := list.GetCurrentItem(), list.GetItemCount()
		offset = max(0, min(offset, count-visible))
		offset = max(0, max(min(offset, current), current-visible+1))
		list.SetOffset(offset, horizontal)
		for i := offset; i < min(count, offset+visible); i++ {
			if search != nil && len(search.matches) == 0 {
				break
			}
			color := tui.Muted
			if i == current {
				color = tui.Foreground
			}
			tview.Print(screen, fmt.Sprintf("%*d", digits, i+1), x+2, row+i-offset, digits, tview.AlignRight, color)
		}
		if count > visible {
			thumb := max(1, visible*visible/count)
			start := (visible - thumb) * offset / (count - visible)
			for index := range visible {
				glyph, color := '│', tui.Border
				if index >= start && index < start+thumb {
					glyph, color = '█', tui.Accent
				}
				screen.SetContent(x+w-2, row+index, glyph, nil, tcell.StyleDefault.Foreground(color).Background(tui.Panel))
			}
		}
		return frame.GetInnerRect()
	})
}

func (h *terminalUI) clearDropdownNumber() {
	if h.dropdownNumberTimer != nil {
		h.dropdownNumberTimer.Stop()
	}
	h.dropdownNumber, h.dropdownNumberTarget, h.dropdownNumberDue = "", nil, time.Time{}
}

func (h *terminalUI) selectDropdownNumber(d *tview.DropDown, digit rune) {
	if h.dropdownNumberTarget != d {
		h.clearDropdownNumber()
	}
	if !d.IsOpen() {
		h.openFocusedDropdown()
	}
	h.dropdownNumberTarget = d
	h.dropdownNumber += string(digit)
	index, err := strconv.Atoi(h.dropdownNumber)
	if err != nil || index < 1 || index > d.GetOptionCount() {
		h.clearDropdownNumber()
		return
	}
	d.Focus(func(p tview.Primitive) { p.(*tview.List).SetCurrentItem(index - 1) })
	if index*10 > d.GetOptionCount() {
		h.commitDropdownNumber()
		return
	}
	// Allow 10, 23, etc. A single digit still selects without requiring Enter.
	h.dropdownNumberDue = time.Now().Add(450 * time.Millisecond)
	if h.dropdownNumberTimer != nil {
		h.dropdownNumberTimer.Stop()
	}
	h.dropdownNumberTimer = time.AfterFunc(460*time.Millisecond, func() { h.dirty.Store(true) })
}

func (h *terminalUI) commitDropdownNumber() {
	d := h.dropdownNumberTarget
	h.clearDropdownNumber()
	if d == nil || !d.IsOpen() || h.focusedDropdown() != d {
		return
	}
	d.Focus(func(p tview.Primitive) {
		p.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(next tview.Primitive) { h.app.SetFocus(next) })
	})
}
