package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DropDown composes a Bubbles-backed List with a compact, styled field.
type DropDown struct {
	*tview.Box
	list                                            *List
	options                                         []string
	current                                         int
	open                                            bool
	label                                           string
	labelWidth, fieldWidth                          int
	fieldStyle, focusedStyle, labelStyle            tcell.Style
	prefix, suffix, fieldPrefix, fieldSuffix, empty string
	selected                                        func(string, int)
}

func NewDropDown() *DropDown {
	d := &DropDown{Box: tview.NewBox().SetBackgroundColor(Panel), list: NewList().SetHighlightFullLine(true), current: -1, fieldStyle: tcell.StyleDefault.Foreground(Foreground).Background(Panel), focusedStyle: Selected, labelStyle: tcell.StyleDefault.Foreground(Accent).Background(Panel)}
	d.list.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey { return e })
	return d
}
func (d *DropDown) SetOptions(options []string, selected func(string, int)) *DropDown {
	d.options = append([]string(nil), options...)
	d.selected = selected
	d.current = -1
	d.list.Clear()
	for _, s := range options {
		d.list.AddItem(d.prefix+s+d.suffix, "", 0, nil)
	}
	return d
}
func (d *DropDown) GetOptionCount() int { return len(d.options) }
func (d *DropDown) SetCurrentOption(n int) *DropDown {
	if n < 0 || n >= len(d.options) {
		d.current = -1
		return d
	}
	d.current = n
	d.list.SetCurrentItem(n)
	if d.selected != nil {
		d.selected(d.options[n], n)
	}
	return d
}
func (d *DropDown) GetCurrentOption() (int, string) {
	if d.current < 0 || d.current >= len(d.options) {
		return -1, ""
	}
	return d.current, d.options[d.current]
}
func (d *DropDown) SetSelectedFunc(f func(string, int)) *DropDown { d.selected = f; return d }
func (d *DropDown) SetLabel(s string) *DropDown                   { d.label = s; return d }
func (d *DropDown) GetLabel() string                              { return d.label }
func (d *DropDown) SetLabelWidth(w int) *DropDown                 { d.labelWidth = w; return d }
func (d *DropDown) SetFieldWidth(w int) *DropDown                 { d.fieldWidth = w; return d }
func (d *DropDown) GetFieldWidth() int                            { return d.fieldWidth }
func (d *DropDown) SetLabelStyle(s tcell.Style) *DropDown         { d.labelStyle = s; return d }
func (d *DropDown) SetFocusedStyle(s tcell.Style) *DropDown       { d.focusedStyle = s; return d }
func (d *DropDown) SetFieldTextColor(c tcell.Color) *DropDown {
	d.fieldStyle = d.fieldStyle.Foreground(c)
	return d
}
func (d *DropDown) SetFieldBackgroundColor(c tcell.Color) *DropDown {
	d.fieldStyle = d.fieldStyle.Background(c)
	return d
}
func (d *DropDown) SetListStyles(normal, selected tcell.Style) *DropDown {
	d.list.SetMainTextStyle(normal).SetSelectedStyle(selected)
	return d
}
func (d *DropDown) SetUseStyleTags(bool) *DropDown { return d }
func (d *DropDown) SetTextOptions(prefix, suffix, fieldPrefix, fieldSuffix, empty string) *DropDown {
	d.prefix, d.suffix, d.fieldPrefix, d.fieldSuffix, d.empty = prefix, suffix, fieldPrefix, fieldSuffix, empty
	return d
}
func (d *DropDown) IsOpen() bool   { return d.open }
func (d *DropDown) HasFocus() bool { return d.Box.HasFocus() || d.open && d.list.HasFocus() }
func (d *DropDown) Focus(f func(tview.Primitive)) {
	if d.open {
		if f != nil {
			f(d.list)
		}
	} else {
		d.Box.Focus(f)
	}
}
func (d *DropDown) Draw(s tcell.Screen) {
	if d.open && !d.HasFocus() {
		d.open = false
	}
	d.Box.DrawForSubclass(s, d)
	x, y, w, h := d.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	lw := d.labelWidth
	if lw <= 0 {
		lw = tview.TaggedStringWidth(d.label)
	}
	lw = min(w, lw)
	fg, _, _ := d.labelStyle.Decompose()
	tview.Print(s, d.label, x, y, lw, tview.AlignLeft, fg)
	x += lw
	w -= lw
	if d.fieldWidth > 0 {
		w = min(w, d.fieldWidth)
	} else {
		width := tview.TaggedStringWidth(d.empty)
		for _, option := range d.options {
			width = max(width, tview.TaggedStringWidth(d.fieldPrefix+option+d.fieldSuffix))
		}
		w = min(w, width)
	}
	if w <= 0 {
		return
	}
	_, label := d.GetCurrentOption()
	if d.current < 0 {
		label = d.empty
	}
	label = d.fieldPrefix + label + d.fieldSuffix
	style := d.fieldStyle
	if d.HasFocus() {
		style = d.focusedStyle
	}
	DrawStyled(s, lipStyle(style).Width(w).Render(""), x, y, w, 1)
	fg, _, _ = style.Decompose()
	tview.Print(s, label, x, y, w, tview.AlignLeft, fg)
	if d.open {
		sw, sh := s.Size()
		width := w
		for _, text := range d.options {
			width = max(width, tview.TaggedStringWidth(text)+2)
		}
		width = min(sw, width)
		height := min(sh, len(d.options))
		top := y + 1
		if top+height > sh {
			top = max(0, y-height)
		}
		d.list.SetRect(max(0, min(x, sw-width)), top, width, height)
		d.list.Draw(s)
	}
}
func (d *DropDown) begin(focus func(tview.Primitive)) {
	if len(d.options) == 0 {
		return
	}
	d.open = true
	d.list.SetCurrentItem(max(0, d.current))
	// Closing restores field focus before running callbacks which may move focus
	// to another field or open another dialog.
	d.list.onSelect = func(n int) { d.open = false; focus(d); d.SetCurrentOption(n) }
	d.list.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyTab || e.Key() == tcell.KeyBacktab {
			d.open = false
			focus(d)
			return nil
		}
		return e
	})
	focus(d.list)
}
func (d *DropDown) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return d.WrapInputHandler(func(e *tcell.EventKey, focus func(tview.Primitive)) {
		if d.open {
			d.list.InputHandler()(e, focus)
			return
		}
		switch e.Key() {
		case tcell.KeyEnter, tcell.KeyDown, tcell.KeyUp:
			d.begin(focus)
		}
	})
}
func (d *DropDown) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return d.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if d.open && d.list.InRect(e.Position()) {
			return d.list.MouseHandler()(a, e, focus)
		}
		if !d.InRect(e.Position()) {
			return false, nil
		}
		if a == tview.MouseLeftDown {
			focus(d)
			if !d.open {
				d.begin(focus)
			}
			return true, nil
		}
		return a == tview.MouseLeftClick, nil
	})
}
