package tui

import (
	"charm.land/bubbles/v2/table"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strings"
)

type TableCell struct {
	Text          string
	MaxWidth      int
	NotSelectable bool
	style         tcell.Style
	selected      *tcell.Style
	reference     any
	clicked       func() bool
	x, y, width   int
}

func NewTableCell(text string) *TableCell {
	return &TableCell{Text: text, style: tcell.StyleDefault.Foreground(Foreground).Background(Panel), x: -1, y: -1}
}
func (c *TableCell) SetText(s string) *TableCell           { c.Text = s; return c }
func (c *TableCell) SetMaxWidth(w int) *TableCell          { c.MaxWidth = w; return c }
func (c *TableCell) SetSelectable(v bool) *TableCell       { c.NotSelectable = !v; return c }
func (c *TableCell) SetTextColor(v tcell.Color) *TableCell { c.style = c.style.Foreground(v); return c }
func (c *TableCell) SetBackgroundColor(v tcell.Color) *TableCell {
	c.style = c.style.Background(v)
	return c
}
func (c *TableCell) SetStyle(v tcell.Style) *TableCell         { c.style = v; return c }
func (c *TableCell) SetSelectedStyle(v tcell.Style) *TableCell { c.selected = &v; return c }
func (c *TableCell) SetReference(v any) *TableCell             { c.reference = v; return c }
func (c *TableCell) GetReference() any                         { return c.reference }
func (c *TableCell) SetClickedFunc(f func() bool) *TableCell   { c.clicked = f; return c }
func (c *TableCell) GetLastPosition() (int, int, int)          { return c.x, c.y, c.width }

// Table uses the Bubbles table model for row navigation. The cell adapter keeps
// fixed headers, per-cell permission styling and clickable session tabs.
type Table struct {
	*tview.Box
	model                                      table.Model
	cells                                      [][]*TableCell
	row, col, columns                          int
	fixedRows, fixedCols, rowOffset, colOffset int
	rowsSelectable, colsSelectable             bool
	selected                                   tcell.Style
	separator                                  rune
	borderColor                                tcell.Color
	changed, chosen                            func(int, int)
	dirty                                      bool
}

func NewTable() *Table {
	return &Table{Box: tview.NewBox().SetBackgroundColor(Panel), model: table.New(table.WithFocused(true)), selected: Selected, separator: ' ', borderColor: Border, dirty: true}
}
func (t *Table) SetSelectable(rows, cols bool) *Table {
	t.rowsSelectable, t.colsSelectable = rows, cols
	return t
}
func (t *Table) SetFixed(rows, cols int) *Table {
	t.fixedRows, t.fixedCols = rows, cols
	t.dirty = true
	return t
}
func (t *Table) SetSelectedStyle(s tcell.Style) *Table           { t.selected = s; return t }
func (t *Table) SetBordersColor(c tcell.Color) *Table            { t.borderColor = c; return t }
func (t *Table) SetSeparator(r rune) *Table                      { t.separator = r; return t }
func (t *Table) SetSelectedFunc(f func(int, int)) *Table         { t.chosen = f; return t }
func (t *Table) SetSelectionChangedFunc(f func(int, int)) *Table { t.changed = f; return t }
func (t *Table) SetCell(row, col int, c *TableCell) *Table {
	for len(t.cells) <= row {
		t.cells = append(t.cells, nil)
	}
	for len(t.cells[row]) <= col {
		t.cells[row] = append(t.cells[row], nil)
	}
	t.cells[row][col] = c
	t.columns = max(t.columns, col+1)
	t.dirty = true
	return t
}
func (t *Table) GetCell(row, col int) *TableCell {
	if row >= 0 && row < len(t.cells) && col >= 0 && col < len(t.cells[row]) && t.cells[row][col] != nil {
		return t.cells[row][col]
	}
	return NewTableCell("").SetSelectable(false)
}
func (t *Table) GetRowCount() int    { return len(t.cells) }
func (t *Table) GetColumnCount() int { return t.columns }
func (t *Table) Clear() *Table {
	t.cells = nil
	t.columns = 0
	t.row, t.col, t.rowOffset, t.colOffset = 0, 0, 0, 0
	t.dirty = true
	return t
}
func (t *Table) GetSelection() (int, int) { return t.row, t.col }
func (t *Table) Select(row, col int) *Table {
	row = max(0, min(row, len(t.cells)-1))
	col = max(0, min(col, t.columns-1))
	changed := row != t.row || col != t.col
	t.row, t.col = row, col
	if changed && t.changed != nil {
		t.changed(row, col)
	}
	return t
}
func (t *Table) GetOffset() (int, int) { return t.rowOffset, t.colOffset }
func (t *Table) SetOffset(row, col int) *Table {
	t.rowOffset = max(0, row)
	t.colOffset = max(0, col)
	return t
}
func (t *Table) ScrollToBeginning() *Table { t.rowOffset, t.colOffset = 0, 0; return t }
func (t *Table) syncModel(height int) {
	if t.dirty {
		columns := make([]table.Column, t.columns)
		for col := range columns {
			columns[col] = table.Column{Title: "", Width: 1}
		}
		t.model.SetColumns(columns)
		rows := make([]table.Row, 0, max(0, len(t.cells)-t.fixedRows))
		for row := t.fixedRows; row < len(t.cells); row++ {
			values := make(table.Row, t.columns)
			for col := range values {
				values[col] = plainTagged(t.GetCell(row, col).Text)
			}
			rows = append(rows, values)
		}
		t.model.SetRows(rows)
		t.dirty = false
	}
	t.model.SetHeight(max(1, height) + 1) // Bubbles reserves one header line; ours is drawn separately.
	t.model.SetCursor(max(0, t.row-t.fixedRows))
}
func (t *Table) Draw(s tcell.Screen) {
	t.Box.DrawForSubclass(s, t)
	x, y, w, h := t.GetInnerRect()
	if w < 1 || h < 1 {
		return
	}
	widths := make([]int, t.columns)
	for _, row := range t.cells {
		for col, c := range row {
			if c == nil {
				continue
			}
			c.x, c.y, c.width = -1, -1, 0
			width := tview.TaggedStringWidth(c.Text)
			if c.MaxWidth > 0 {
				width = min(width, c.MaxWidth)
			}
			widths[col] = max(widths[col], width)
		}
	}
	t.rowOffset = min(t.rowOffset, max(0, len(t.cells)-t.fixedRows-max(1, h-t.fixedRows)))
	for line := 0; line < h; line++ {
		row := line
		if line >= t.fixedRows {
			row += t.rowOffset
		}
		if row >= len(t.cells) {
			break
		}
		left := x
		for col, width := range widths {
			if col >= t.fixedCols && col < t.fixedCols+t.colOffset || width == 0 {
				continue
			}
			if left >= x+w {
				break
			}
			c := t.GetCell(row, col)
			width = min(width, x+w-left)
			style := c.style
			selected := !c.NotSelectable && (t.rowsSelectable && row == t.row || t.colsSelectable && col == t.col)
			if selected {
				style = t.selected
				if c.selected != nil {
					style = *c.selected
				}
			}
			DrawStyled(s, lipStyle(style).Width(width).Render(strings.Repeat(" ", width)), left, y+line, width, 1)
			fg, _, _ := style.Decompose()
			tview.Print(s, c.Text, left, y+line, width, tview.AlignLeft, fg)
			c.x, c.y, c.width = left, y+line, width
			left += width
			if left < x+w {
				s.SetContent(left, y+line, t.separator, nil, tcell.StyleDefault.Foreground(t.borderColor).Background(t.GetBackgroundColor()))
				left++
			}
		}
	}
}
func (t *Table) CellAt(x, y int) (int, int) {
	for row, cells := range t.cells {
		for col, c := range cells {
			if c != nil && c.width > 0 && y == c.y && x >= c.x && x < c.x+c.width {
				return row, col
			}
		}
	}
	return -1, -1
}
func (t *Table) reveal() {
	_, _, _, h := t.GetInnerRect()
	h = max(1, h-t.fixedRows)
	if t.row < t.fixedRows+t.rowOffset {
		t.rowOffset = max(0, t.row-t.fixedRows)
	}
	if t.row >= t.fixedRows+t.rowOffset+h {
		t.rowOffset = t.row - t.fixedRows - h + 1
	}
}
func (t *Table) activate() {
	if !t.GetCell(t.row, t.col).NotSelectable && t.chosen != nil {
		t.chosen(t.row, t.col)
	}
}
func (t *Table) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return t.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		if e.Key() == tcell.KeyEnter {
			t.activate()
			return
		}
		if e.Key() == tcell.KeyLeft || e.Key() == tcell.KeyRight {
			step := 1
			if e.Key() == tcell.KeyLeft {
				step = -1
			}
			if t.colsSelectable {
				t.Select(t.row, t.col+step)
			} else {
				t.colOffset = max(0, min(t.columns-1, t.colOffset+step))
			}
			return
		}
		if !t.rowsSelectable {
			return
		}
		_, _, _, h := t.GetInnerRect()
		t.syncModel(h - t.fixedRows)
		if msg, ok := bubbleKey(e); ok {
			t.model, _ = t.model.Update(msg)
			t.Select(t.model.Cursor()+t.fixedRows, t.col)
			t.reveal()
		}
	})
}
func (t *Table) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return t.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if !t.InRect(e.Position()) {
			return false, nil
		}
		switch a {
		case tview.MouseScrollUp, tview.MouseScrollDown:
			step := 1
			if a == tview.MouseScrollUp {
				step = -1
			}
			if t.rowsSelectable {
				t.Select(max(t.fixedRows, t.row+step), t.col)
				t.reveal()
			}
			return true, nil
		case tview.MouseLeftDown, tview.MouseLeftClick:
			row, col := t.CellAt(e.Position())
			if row < 0 || t.GetCell(row, col).NotSelectable {
				return false, nil
			}
			focus(t)
			t.Select(row, col)
			if a == tview.MouseLeftClick {
				if f := t.GetCell(row, col).clicked; f != nil {
					f()
				}
			}
			return true, nil
		}
		return false, nil
	})
}
