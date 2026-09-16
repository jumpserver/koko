package tui

import (
	"charm.land/bubbles/v2/list"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strings"
)

type choice struct {
	text, secondary string
	shortcut        rune
	run             func()
}

func (c choice) FilterValue() string { return c.text }

// List uses Bubbles for selection/navigation; cell painting preserves the
// workspace's tagged labels and per-session palette during the migration.
type List struct {
	*tview.Box
	model               list.Model
	normal, selected    tcell.Style
	offset, horizontal  int
	secondary, fullLine bool
	onSelect            func(int)
}

func NewList() *List {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	m := list.New(nil, d, 1, 1)
	m.SetFilteringEnabled(false)
	m.SetShowTitle(false)
	m.SetShowHelp(false)
	m.SetShowStatusBar(false)
	m.SetShowPagination(false)
	m.DisableQuitKeybindings()
	return &List{Box: tview.NewBox().SetBackgroundColor(Panel), model: m, normal: tcell.StyleDefault.Foreground(Foreground).Background(Panel), selected: Selected}
}
func (l *List) ShowSecondaryText(v bool) *List       { l.secondary = v; return l }
func (l *List) SetHighlightFullLine(v bool) *List    { l.fullLine = v; return l }
func (l *List) SetMainTextStyle(s tcell.Style) *List { l.normal = s; return l }
func (l *List) SetSelectedStyle(s tcell.Style) *List { l.selected = s; return l }
func (l *List) AddItem(text, secondary string, shortcut rune, run func()) *List {
	n := l.model.Index()
	items := append(l.model.Items(), choice{text, secondary, shortcut, run})
	l.model.SetItems(items)
	l.model.Select(n)
	return l
}
func (l *List) Clear() *List      { l.model.SetItems(nil); l.offset = 0; return l }
func (l *List) GetItemCount() int { return len(l.model.Items()) }
func (l *List) GetItemText(n int) (string, string) {
	if n < 0 || n >= l.GetItemCount() {
		return "", ""
	}
	c := l.model.Items()[n].(choice)
	return c.text, c.secondary
}
func (l *List) GetCurrentItem() int { return l.model.Index() }
func (l *List) SetCurrentItem(n int) *List {
	l.model.Select(max(0, min(n, l.GetItemCount()-1)))
	return l
}
func (l *List) GetOffset() (int, int) { return l.offset, l.horizontal }
func (l *List) SetOffset(row, col int) *List {
	l.offset = max(0, row)
	l.horizontal = max(0, col)
	return l
}
func (l *List) Draw(s tcell.Screen) {
	l.Box.DrawForSubclass(s, l)
	x, y, w, h := l.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	l.model.SetSize(w, h)
	n := l.model.Index()
	l.offset = max(0, min(l.offset, max(0, l.GetItemCount()-h)))
	if n < l.offset {
		l.offset = n
	}
	if n >= l.offset+h {
		l.offset = n - h + 1
	}
	for row := 0; row < h && row+l.offset < l.GetItemCount(); row++ {
		idx := row + l.offset
		text, _ := l.GetItemText(idx)
		style := l.normal
		if idx == n {
			style = l.selected
		}
		width := w
		if !l.fullLine {
			width = min(w, tview.TaggedStringWidth(text))
		}
		DrawStyled(s, lipStyle(style).Width(width).Render(strings.Repeat(" ", width)), x, y+row, width, 1)
		fg, _, _ := style.Decompose()
		tview.Print(s, text, x, y+row, w, tview.AlignLeft, fg)
	}
}
func (l *List) activate() {
	n := l.model.Index()
	if n < 0 || n >= l.GetItemCount() {
		return
	}
	c := l.model.Items()[n].(choice)
	if l.onSelect != nil {
		l.onSelect(n)
	}
	if c.run != nil {
		c.run()
	}
}
func (l *List) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return l.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		if e.Key() == tcell.KeyEnter {
			l.activate()
			return
		}
		if e.Key() == tcell.KeyRune {
			for n, item := range l.model.Items() {
				c := item.(choice)
				if c.shortcut != 0 && c.shortcut == e.Rune() {
					l.SetCurrentItem(n)
					l.activate()
					return
				}
			}
		}
		if msg, ok := bubbleKey(e); ok {
			l.model, _ = l.model.Update(msg)
		}
	})
}
func (l *List) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return l.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if !l.InRect(e.Position()) {
			return false, nil
		}
		_, y, _, h := l.GetInnerRect()
		_, my := e.Position()
		switch a {
		case tview.MouseScrollUp:
			l.SetCurrentItem(l.model.Index() - 1)
		case tview.MouseScrollDown:
			l.SetCurrentItem(l.model.Index() + 1)
		case tview.MouseLeftDown, tview.MouseLeftClick:
			if my < y || my >= y+h || my-y+l.offset >= l.GetItemCount() {
				return false, nil
			}
			focus(l)
			l.SetCurrentItem(my - y + l.offset)
			if a == tview.MouseLeftClick {
				l.activate()
			}
		default:
			return false, nil
		}
		return true, nil
	})
}
