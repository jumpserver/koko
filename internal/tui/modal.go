package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Modal composes the migrated text viewport and buttons; it owns its local
// focus cycle so destructive confirmations keep the same keyboard behavior.
type Modal struct {
	*tview.Box
	text           *TextView
	buttons        []*Button
	selected       int
	done           func(int, string)
	normal, active tcell.Style
}

func NewModal() *Modal {
	m := &Modal{Box: tview.NewBox().SetBackgroundColor(Panel), text: NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter), normal: tcell.StyleDefault.Foreground(Muted).Background(Raised), active: Selected}
	m.SetBorder(true)
	m.SetBorderPadding(1, 1, 2, 2)
	return m
}
func (m *Modal) SetText(s string) *Modal { m.text.SetText(s); return m }
func (m *Modal) AddButtons(labels []string) *Modal {
	for _, s := range labels {
		n := len(m.buttons)
		b := NewButton(s).SetStyle(m.normal).SetActivatedStyle(m.active)
		b.SetSelectedFunc(func() {
			if m.done != nil {
				m.done(n, m.buttons[n].GetLabel())
			}
		})
		m.buttons = append(m.buttons, b)
	}
	return m
}
func (m *Modal) SetDoneFunc(f func(int, string)) *Modal { m.done = f; return m }
func (m *Modal) SetBackgroundColor(c tcell.Color) *Modal {
	m.Box.SetBackgroundColor(c)
	m.text.SetBackgroundColor(c)
	return m
}
func (m *Modal) SetTextColor(c tcell.Color) *Modal {
	m.text.SetTextStyle(tcell.StyleDefault.Foreground(c).Background(m.GetBackgroundColor()))
	return m
}
func (m *Modal) SetButtonStyle(s tcell.Style) *Modal {
	m.normal = s
	for _, b := range m.buttons {
		b.SetStyle(s)
	}
	return m
}
func (m *Modal) SetButtonActivatedStyle(s tcell.Style) *Modal {
	m.active = s
	for _, b := range m.buttons {
		b.SetActivatedStyle(s)
	}
	return m
}
func (m *Modal) Draw(s tcell.Screen) {
	x, y, w, h := m.GetRect()
	width := min(w, 70)
	m.text.SetSize(0, max(1, width-6))
	height := min(h, m.text.GetWrappedLineCount()+7)
	m.Box.SetRect(x+(w-width)/2, y+(h-height)/2, width, height)
	m.Box.DrawForSubclass(s, m)
	ix, iy, iw, ih := m.GetInnerRect()
	m.text.SetRect(ix, iy, iw, max(0, ih-2))
	m.text.Draw(s)
	n := len(m.buttons)
	if n == 0 {
		return
	}
	buttonWidth := max(1, (iw-max(0, n-1))/n)
	for index, b := range m.buttons {
		b.SetRect(ix+index*(buttonWidth+1), iy+ih-1, buttonWidth, 1)
		if index == m.selected && m.HasFocus() {
			b.Focus(nil)
		} else {
			b.Blur()
		}
		b.Draw(s)
	}
}
func (m *Modal) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return m.WrapInputHandler(func(e *tcell.EventKey, focus func(tview.Primitive)) {
		if len(m.buttons) == 0 {
			return
		}
		switch e.Key() {
		case tcell.KeyTab, tcell.KeyRight, tcell.KeyDown:
			m.selected = (m.selected + 1) % len(m.buttons)
		case tcell.KeyBacktab, tcell.KeyLeft, tcell.KeyUp:
			m.selected = (m.selected + len(m.buttons) - 1) % len(m.buttons)
		case tcell.KeyEnter:
			m.buttons[m.selected].activate()
		case tcell.KeyEscape:
			if m.done != nil {
				m.done(-1, "")
			}
		default:
			m.text.InputHandler()(e, focus)
		}
	})
}
func (m *Modal) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return m.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		for index, b := range m.buttons {
			if b.InRect(e.Position()) {
				m.selected = index
				return b.MouseHandler()(a, e, func(tview.Primitive) { focus(m) })
			}
		}
		return m.text.MouseHandler()(a, e, func(tview.Primitive) { focus(m) })
	})
}
