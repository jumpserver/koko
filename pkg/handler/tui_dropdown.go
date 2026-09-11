package handler

import (
	"fmt"
	"strconv"
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
	frame := tview.NewBox().SetBorderPadding(1, 1, digits+3, 2)
	tuiBorder(frame, "", tui.FocusBorder)
	// Flex supplies a transparent Box, so the native, unbounded rectangle is
	// never cleared. Keep the native input capture and delegated list focus.
	capture, focused := list.GetInputCapture(), list.HasFocus()
	list.Box = tview.NewFlex().Box
	list.SetInputCapture(capture)
	search := h.accountSearchFor(d)
	if search != nil {
		list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			switch ev.Key() {
			case tcell.KeyRune, tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete, tcell.KeyLeft, tcell.KeyRight, tcell.KeyCtrlW:
				search.field.InputHandler()(ev, func(tview.Primitive) {})
				return nil
			case tcell.KeyCtrlU:
				search.field.SetText("")
				return nil
			case tcell.KeyEnter:
				if len(search.matches) == 0 {
					return nil
				}
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
		height := min(min(list.GetItemCount(), tuiDropdownVisibleItems)+4, max(below, above))
		y := dy + 1
		if below < height {
			y = dy - height
		}
		list.SetRect(x, y, w, height)
		digits := len(strconv.Itoa(list.GetItemCount()))
		frame.SetBorderPadding(1, 1, digits+3, 2)
		frame.SetRect(x, y, w, height)
		frame.SetBackgroundColor(tui.Panel).SetBorderColor(tui.FocusBorder)
		frame.Draw(screen)
		if search != nil {
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
		visible := max(0, height-4)
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
			tview.Print(screen, fmt.Sprintf("%*d", digits, i+1), x+2, y+2+i-offset, digits, tview.AlignRight, color)
		}
		if count > visible {
			thumb := max(1, visible*visible/count)
			start := (visible - thumb) * offset / (count - visible)
			for row := range visible {
				glyph, color := '│', tui.Border
				if row >= start && row < start+thumb {
					glyph, color = '█', tui.Accent
				}
				screen.SetContent(x+w-2, y+2+row, glyph, nil, tcell.StyleDefault.Foreground(color).Background(tui.Panel))
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
