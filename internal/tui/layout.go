package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type flexItem struct {
	child         tview.Primitive
	fixed, weight int
	focus         bool
}

// Flex retains the current row/column sizing contract while removing tview's
// layout and event dispatch. Only its transparent Box base is reused.
type Flex struct {
	*tview.Box
	items     []flexItem
	direction int
}

func NewFlex() *Flex                     { return &Flex{Box: tview.NewFlex().Box, direction: tview.FlexColumn} }
func (f *Flex) SetDirection(d int) *Flex { f.direction = d; return f }
func (f *Flex) AddItem(p tview.Primitive, fixed, weight int, focus bool) *Flex {
	f.items = append(f.items, flexItem{p, fixed, weight, focus})
	return f
}
func (f *Flex) RemoveItem(p tview.Primitive) *Flex {
	for n := 0; n < len(f.items); {
		if f.items[n].child == p {
			f.items = append(f.items[:n], f.items[n+1:]...)
		} else {
			n++
		}
	}
	return f
}
func (f *Flex) Clear() *Flex { f.items = nil; return f }
func (f *Flex) ResizeItem(p tview.Primitive, fixed, weight int) *Flex {
	for n := range f.items {
		if f.items[n].child == p {
			f.items[n].fixed, f.items[n].weight = fixed, weight
			break
		}
	}
	return f
}
func (f *Flex) HasFocus() bool {
	if f.Box.HasFocus() {
		return true
	}
	for _, item := range f.items {
		if item.child != nil && item.child.HasFocus() {
			return true
		}
	}
	return false
}
func (f *Flex) Focus(delegate func(tview.Primitive)) {
	for _, item := range f.items {
		if item.child != nil && item.focus && delegate != nil {
			delegate(item.child)
			return
		}
	}
	f.Box.Focus(delegate)
}
func (f *Flex) Draw(s tcell.Screen) {
	f.Box.DrawForSubclass(s, f)
	x, y, w, h := f.GetInnerRect()
	available := w
	if f.direction == tview.FlexRow {
		available = h
	}
	available = max(0, available)
	remaining, weight := available, 0
	for _, item := range f.items {
		if item.fixed > 0 {
			remaining -= item.fixed
		} else {
			weight += item.weight
		}
	}
	remaining = max(0, remaining)
	offset := 0
	for _, item := range f.items {
		size := item.fixed
		if size <= 0 {
			if weight > 0 {
				size = remaining * item.weight / weight
			}
			remaining -= size
			weight -= item.weight
		}
		size = max(0, min(size, available-offset))
		if item.child != nil {
			if f.direction == tview.FlexRow {
				item.child.SetRect(x, y+offset, w, size)
			} else {
				item.child.SetRect(x+offset, y, size, h)
			}
			if size > 0 {
				item.child.Draw(s)
			}
		}
		offset += size
	}
}
func (f *Flex) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return f.WrapInputHandler(func(e *tcell.EventKey, focus func(tview.Primitive)) {
		for _, item := range f.items {
			if item.child != nil && item.child.HasFocus() {
				if h := item.child.InputHandler(); h != nil {
					h(e, focus)
				}
				return
			}
		}
	})
}
func (f *Flex) PasteHandler() func(string, func(tview.Primitive)) {
	return f.WrapPasteHandler(func(s string, focus func(tview.Primitive)) {
		for _, item := range f.items {
			if item.child != nil && item.child.HasFocus() {
				if h := item.child.PasteHandler(); h != nil {
					h(s, focus)
				}
				return
			}
		}
	})
}
func (f *Flex) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return f.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		for _, item := range f.items {
			if item.child != nil {
				if h := item.child.MouseHandler(); h != nil {
					if used, capture := h(a, e, focus); used || capture != nil {
						return used, capture
					}
				}
			}
		}
		return false, nil
	})
}

type page struct {
	name            string
	child           tview.Primitive
	resize, visible bool
}
type Pages struct {
	*tview.Box
	pages []page
}

func NewPages() *Pages { return &Pages{Box: tview.NewBox().SetBackgroundColor(Panel)} }
func (p *Pages) AddPage(name string, child tview.Primitive, resize, visible bool) *Pages {
	p.RemovePage(name)
	p.pages = append(p.pages, page{name, child, resize, visible})
	return p
}
func (p *Pages) RemovePage(name string) *Pages {
	for n, item := range p.pages {
		if item.name == name {
			p.pages = append(p.pages[:n], p.pages[n+1:]...)
			break
		}
	}
	return p
}
func (p *Pages) GetPage(name string) tview.Primitive {
	for _, item := range p.pages {
		if item.name == name {
			return item.child
		}
	}
	return nil
}
func (p *Pages) SwitchToPage(name string) *Pages {
	for n := range p.pages {
		p.pages[n].visible = p.pages[n].name == name
	}
	return p
}
func (p *Pages) active() tview.Primitive {
	for n := len(p.pages) - 1; n >= 0; n-- {
		if p.pages[n].visible {
			return p.pages[n].child
		}
	}
	return nil
}
func (p *Pages) HasFocus() bool {
	if child := p.active(); child != nil {
		return child.HasFocus()
	}
	return p.Box.HasFocus()
}
func (p *Pages) Focus(delegate func(tview.Primitive)) {
	if child := p.active(); child != nil && delegate != nil {
		delegate(child)
	} else {
		p.Box.Focus(delegate)
	}
}
func (p *Pages) Draw(s tcell.Screen) {
	p.Box.DrawForSubclass(s, p)
	x, y, w, h := p.GetInnerRect()
	for _, item := range p.pages {
		if item.visible {
			if item.resize {
				item.child.SetRect(x, y, w, h)
			}
			item.child.Draw(s)
		}
	}
}
func (p *Pages) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return p.WrapInputHandler(func(e *tcell.EventKey, focus func(tview.Primitive)) {
		if child := p.active(); child != nil {
			if h := child.InputHandler(); h != nil {
				h(e, focus)
			}
		}
	})
}
func (p *Pages) PasteHandler() func(string, func(tview.Primitive)) {
	return p.WrapPasteHandler(func(s string, focus func(tview.Primitive)) {
		if child := p.active(); child != nil {
			if h := child.PasteHandler(); h != nil {
				h(s, focus)
			}
		}
	})
}
func (p *Pages) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if child := p.active(); child != nil {
			if h := child.MouseHandler(); h != nil {
				return h(a, e, focus)
			}
		}
		return false, nil
	})
}
