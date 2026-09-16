package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strings"
)

// Button has no tview control state. Its Box adapts the workspace layout while
// Lip Gloss and the Bubble Tea event loop own appearance and activation.
type Button struct {
	*tview.Box
	label                         string
	normal, active, disabledStyle tcell.Style
	disabled                      bool
	selected                      func()
}

func NewButton(label string) *Button {
	return &Button{Box: tview.NewBox().SetBackgroundColor(Panel), label: label, normal: tcell.StyleDefault.Foreground(Foreground).Background(Panel), active: Selected, disabledStyle: tcell.StyleDefault.Foreground(Disabled).Background(Panel)}
}
func (b *Button) SetLabel(label string) *Button           { b.label = label; return b }
func (b *Button) GetLabel() string                        { return b.label }
func (b *Button) SetStyle(s tcell.Style) *Button          { b.normal = s; return b }
func (b *Button) SetActivatedStyle(s tcell.Style) *Button { b.active = s; return b }
func (b *Button) SetDisabledStyle(s tcell.Style) *Button  { b.disabledStyle = s; return b }
func (b *Button) SetDisabled(v bool) *Button              { b.disabled = v; return b }
func (b *Button) IsDisabled() bool                        { return b.disabled }
func (b *Button) SetSelectedFunc(f func()) *Button        { b.selected = f; return b }
func (b *Button) Draw(s tcell.Screen) {
	b.Box.DrawForSubclass(s, b)
	x, y, w, h := b.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	style := b.normal
	if b.disabled {
		style = b.disabledStyle
	} else if b.HasFocus() {
		style = b.active
	}
	y += (h - 1) / 2
	DrawStyled(s, lipStyle(style).Width(w).Render(strings.Repeat(" ", w)), x, y, w, 1)
	fg, _, _ := style.Decompose()
	tview.Print(s, b.label, x, y, w, tview.AlignCenter, fg)
}
func (b *Button) activate() {
	if !b.disabled && b.selected != nil {
		b.selected()
	}
}
func (b *Button) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return b.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		if e.Key() == tcell.KeyEnter || e.Key() == tcell.KeyRune && e.Rune() == ' ' {
			b.activate()
		}
	})
}
func (b *Button) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return b.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if b.disabled || !b.InRect(e.Position()) {
			return false, nil
		}
		switch a {
		case tview.MouseLeftDown:
			focus(b)
			return true, nil
		case tview.MouseLeftClick:
			focus(b)
			b.activate()
			return true, nil
		}
		return false, nil
	})
}
