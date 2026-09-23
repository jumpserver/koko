package handler

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tview handles vertical wheel/PageUp/PageDown and keyboard horizontal scrolling.
// Add horizontal wheel gestures too; row selection is unchanged while scrolling.
func enableTableScroll(table *tview.Table) {
	previousCapture := table.GetMouseCapture()
	table.SetMouseCapture(func(a tview.MouseAction, e *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if previousCapture != nil {
			a, e = previousCapture(a, e)
			if e == nil {
				return a, nil
			}
		}
		x, y := e.Position()
		if !table.InRect(x, y) {
			return a, e
		}
		if a == tview.MouseLeftDoubleClick {
			row, col := table.CellAt(x, y)
			if row > 0 {
				table.Select(row, col)
				table.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(tview.Primitive) {})
			}
			return tview.MouseConsumed, nil
		}
		delta := 0
		if a == tview.MouseScrollLeft {
			delta = -1
		}
		if a == tview.MouseScrollRight {
			delta = 1
		}
		if e.Modifiers()&tcell.ModShift != 0 {
			if a == tview.MouseScrollUp {
				delta = -1
			}
			if a == tview.MouseScrollDown {
				delta = 1
			}
		}
		if delta != 0 {
			row, col := table.GetOffset()
			table.SetOffset(row, min(max(0, table.GetColumnCount()-1), max(0, col+delta)))
			return tview.MouseConsumed, nil
		}
		return a, e
	})
}
