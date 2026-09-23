package handler

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tuiOverlay confines every child (including its mouse hit area) to a centered
// rectangle, recomputed after each outer terminal resize.
type tuiOverlay struct {
	*tview.Box
	child          tview.Primitive
	width, height  int
	anchor         tview.Primitive
	paste          func(string, func(tview.Primitive))
	dismissOutside func()
}

func (o *tuiOverlay) Draw(s tcell.Screen) {
	x, y, w, h := o.GetRect()
	width, height := min(o.width, max(1, w-4)), min(o.height, max(1, h-2))
	if o.width == 0 {
		width = max(1, w-4)
	}
	if o.height == 0 {
		height = max(1, h-2)
	}
	left, top := x+(w-width)/2, y+(h-height)/2
	if o.anchor != nil {
		ax, ay, aw, ah := o.anchor.GetRect()
		left, top = max(x, ax+aw-width), max(0, ay+ah)
	}
	o.child.SetRect(left, top, width, height)
	o.child.Draw(s)
}
func (o *tuiOverlay) Focus(f func(tview.Primitive)) { f(o.child) }
func (o *tuiOverlay) HasFocus() bool                { return o.child.HasFocus() }
func (o *tuiOverlay) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return o.child.InputHandler()
}
func (o *tuiOverlay) PasteHandler() func(string, func(tview.Primitive)) {
	if o.paste != nil {
		return o.paste
	}
	return o.child.PasteHandler()
}
func (o *tuiOverlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(a tview.MouseAction, e *tcell.EventMouse, f func(tview.Primitive)) (bool, tview.Primitive) {
		if o.dismissOutside != nil && a == tview.MouseLeftDown {
			mx, my := e.Position()
			x, y, width, height := o.child.GetRect()
			if mx < x || mx >= x+width || my < y || my >= y+height {
				o.dismissOutside()
				return true, o
			}
		}
		if handler := o.child.MouseHandler(); handler != nil {
			_, capture := handler(a, e, f)
			// Native dropdowns capture the mouse while their list is open. Keep
			// that capture even when the list extends beyond the dialog bounds.
			return true, capture
		}
		// The underlying asset list must never receive clicks through a modal.
		return true, nil
	}
}

func (h *terminalUI) dismissModal() {
	if len(h.dialogs) == 0 {
		return
	}
	h.detailGeneration++
	last := h.dialogs[len(h.dialogs)-1]
	h.pages.RemovePage(last.page)
	h.dialogs = h.dialogs[:len(h.dialogs)-1]
	h.modal = len(h.dialogs) > 0
	if last.returnFocus != nil {
		h.app.SetFocus(last.returnFocus)
	}
	h.setWindowHelp()
}
