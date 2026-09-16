package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strings"
)

type TreeNode struct {
	text                 string
	children             []*TreeNode
	parent               *TreeNode
	reference            any
	expanded, selectable bool
	style, selected      tcell.Style
}

func NewTreeNode(text string) *TreeNode {
	return &TreeNode{text: text, selectable: true, style: tcell.StyleDefault.Foreground(Foreground).Background(Panel), selected: Selected}
}
func (n *TreeNode) SetText(s string) *TreeNode                   { n.text = s; return n }
func (n *TreeNode) GetText() string                              { return n.text }
func (n *TreeNode) SetTextStyle(s tcell.Style) *TreeNode         { n.style = s; return n }
func (n *TreeNode) SetSelectedTextStyle(s tcell.Style) *TreeNode { n.selected = s; return n }
func (n *TreeNode) SetReference(v any) *TreeNode                 { n.reference = v; return n }
func (n *TreeNode) GetReference() any                            { return n.reference }
func (n *TreeNode) SetSelectable(v bool) *TreeNode               { n.selectable = v; return n }
func (n *TreeNode) SetExpanded(v bool) *TreeNode                 { n.expanded = v; return n }
func (n *TreeNode) IsExpanded() bool                             { return n.expanded }
func (n *TreeNode) GetChildren() []*TreeNode                     { return n.children }
func (n *TreeNode) AddChild(c *TreeNode) *TreeNode {
	c.parent = n
	n.children = append(n.children, c)
	return n
}
func (n *TreeNode) SetChildren(children []*TreeNode) *TreeNode {
	n.ClearChildren()
	for _, c := range children {
		n.AddChild(c)
	}
	return n
}
func (n *TreeNode) ClearChildren() *TreeNode {
	for _, c := range n.children {
		c.parent = nil
	}
	n.children = nil
	return n
}
func (n *TreeNode) Walk(f func(*TreeNode, *TreeNode) bool) {
	if !f(n, n.parent) {
		return
	}
	for _, c := range n.children {
		c.Walk(f)
	}
}

type treeRow struct {
	node   *TreeNode
	depth  int
	prefix string
}

// TreeView keeps the lazy-loaded node graph separate from its visible rows.
// Bubbles has no tree component; navigation and connectors are local to Koko.
type TreeView struct {
	*tview.Box
	root, current *TreeNode
	top, offset   int
	graphics      tcell.Color
	chosen        func(*TreeNode)
}

func NewTreeView() *TreeView {
	return &TreeView{Box: tview.NewBox().SetBackgroundColor(Panel), graphics: Border}
}
func (t *TreeView) SetGraphicsColor(c tcell.Color) *TreeView    { t.graphics = c; return t }
func (t *TreeView) SetRoot(n *TreeNode) *TreeView               { t.root = n; t.current = n; t.offset = 0; return t }
func (t *TreeView) GetRoot() *TreeNode                          { return t.root }
func (t *TreeView) SetTopLevel(n int) *TreeView                 { t.top = max(0, n); return t }
func (t *TreeView) SetCurrentNode(n *TreeNode) *TreeView        { t.current = n; return t }
func (t *TreeView) GetCurrentNode() *TreeNode                   { return t.current }
func (t *TreeView) SetSelectedFunc(f func(*TreeNode)) *TreeView { t.chosen = f; return t }
func (t *TreeView) GetPath(n *TreeNode) []*TreeNode {
	var path []*TreeNode
	for n != nil {
		path = append(path, n)
		n = n.parent
	}
	for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
		path[a], path[b] = path[b], path[a]
	}
	return path
}
func (t *TreeView) visible() []treeRow {
	var rows []treeRow
	var visit func(*TreeNode, int, string, bool)
	visit = func(n *TreeNode, depth int, prefix string, last bool) {
		connector := "├─ "
		if last {
			connector = "└─ "
		}
		p := prefix + connector
		if depth <= t.top {
			p = ""
		}
		if depth >= t.top {
			rows = append(rows, treeRow{n, max(0, depth-t.top), p})
		}
		if !n.expanded && depth >= t.top {
			return
		}
		next := prefix
		if depth >= t.top {
			if depth > t.top {
				if last {
					next += "   "
				} else {
					next += "│  "
				}
			}
		}
		for index, c := range n.children {
			visit(c, depth+1, next, index == len(n.children)-1)
		}
	}
	if t.root != nil {
		visit(t.root, 0, "", true)
	}
	return rows
}
func (t *TreeView) GetRowCount() int     { return len(t.visible()) }
func (t *TreeView) GetScrollOffset() int { return t.offset }
func (t *TreeView) Move(step int) *TreeView {
	rows := t.visible()
	index := 0
	for n, row := range rows {
		if row.node == t.current {
			index = n
			break
		}
	}
	index = max(0, min(len(rows)-1, index+step))
	direction := 1
	if step < 0 {
		direction = -1
	}
	for index >= 0 && index < len(rows) {
		if rows[index].node.selectable {
			t.current = rows[index].node
			break
		}
		index += direction
	}
	return t
}
func (t *TreeView) Draw(s tcell.Screen) {
	t.Box.DrawForSubclass(s, t)
	x, y, w, h := t.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	rows := t.visible()
	selected := -1
	for n, row := range rows {
		if row.node == t.current {
			selected = n
			break
		}
	}
	if selected < 0 && t.current != nil {
		for parent := t.current.parent; parent != nil; parent = parent.parent {
			for n, row := range rows {
				if row.node == parent && parent.selectable {
					t.current = parent
					selected = n
					break
				}
			}
			if selected >= 0 {
				break
			}
		}
	}
	t.offset = max(0, min(t.offset, max(0, len(rows)-h)))
	if selected >= 0 {
		if selected < t.offset {
			t.offset = selected
		}
		if selected >= t.offset+h {
			t.offset = selected - h + 1
		}
	}
	for row := 0; row < h && row+t.offset < len(rows); row++ {
		item := rows[row+t.offset]
		prefix := item.prefix
		tview.Print(s, prefix, x, y+row, w, tview.AlignLeft, t.graphics)
		indent := min(w, item.depth*3)
		style := item.node.style
		if item.node == t.current {
			style = item.node.selected
		}
		width := min(w-indent, tview.TaggedStringWidth(item.node.text))
		DrawStyled(s, lipStyle(style).Width(width).Render(strings.Repeat(" ", width)), x+indent, y+row, width, 1)
		fg, _, _ := style.Decompose()
		tview.Print(s, item.node.text, x+indent, y+row, width, tview.AlignLeft, fg)
	}
}
func (t *TreeView) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return t.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		_, _, _, h := t.GetInnerRect()
		switch e.Key() {
		case tcell.KeyUp:
			t.Move(-1)
		case tcell.KeyDown:
			t.Move(1)
		case tcell.KeyPgUp:
			t.Move(-max(1, h))
		case tcell.KeyPgDn:
			t.Move(max(1, h))
		case tcell.KeyHome:
			t.Move(-t.GetRowCount())
		case tcell.KeyEnd:
			t.Move(t.GetRowCount())
		case tcell.KeyEnter:
			if t.current != nil && t.chosen != nil {
				t.chosen(t.current)
			}
		case tcell.KeyRune:
			switch e.Rune() {
			case 'j':
				t.Move(1)
			case 'k':
				t.Move(-1)
			case 'g':
				t.Move(-t.GetRowCount())
			case 'G':
				t.Move(t.GetRowCount())
			case 'J':
				if t.current != nil && len(t.current.children) > 0 {
					t.current.expanded = true
					t.current = t.current.children[0]
				}
			case 'K':
				if t.current != nil && t.current.parent != nil && t.current.parent.selectable {
					t.current = t.current.parent
				}
			}
		}
	})
}
func (t *TreeView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return t.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if !t.InRect(e.Position()) {
			return false, nil
		}
		_, y, _, h := t.GetInnerRect()
		_, my := e.Position()
		switch a {
		case tview.MouseScrollUp:
			t.offset = max(0, t.offset-1)
		case tview.MouseScrollDown:
			t.offset = min(max(0, t.GetRowCount()-h), t.offset+1)
		case tview.MouseLeftDown, tview.MouseLeftClick:
			rows := t.visible()
			n := my - y + t.offset
			if my < y || my >= y+h || n >= len(rows) || !rows[n].node.selectable {
				return false, nil
			}
			focus(t)
			t.current = rows[n].node
			if a == tview.MouseLeftClick && t.chosen != nil {
				t.chosen(t.current)
			}
		default:
			return false, nil
		}
		return true, nil
	})
}
