package handler

import (
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (h *terminalUI) focusedDropdown() *tview.DropDown {
	for _, item := range h.focusOrder() {
		if d, ok := item.(*tview.DropDown); ok && d.HasFocus() {
			return d
		}
	}
	return nil
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
		d.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { h.app.SetFocus(p) })
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
