package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"testing"
)

func TestInputViewportAndDelegatedPaste(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	placeholder := NewInputField().SetPlaceholder("搜索资产")
	placeholder.SetRect(0, 0, 12, 1)
	placeholder.Draw(s)
	if r, _, _, _ := s.GetContent(6, 0); r != '产' {
		t.Fatalf("truncated placeholder: %q", r)
	}
	placeholder.Focus(nil)
	for _, r := range "中文abc" {
		placeholder.InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, 0), func(tview.Primitive) {})
		placeholder.Draw(s)
	}
	for col, want := range map[int]rune{0: '中', 2: '文', 4: 'a', 5: 'b', 6: 'c'} {
		if r, _, _, _ := s.GetContent(col, 0); r != want {
			t.Fatalf("input column %d: got %q, want %q", col, r, want)
		}
	}
	input := NewInputField().SetMaxLength(6).SetText("中文abc")
	input.SetRect(0, 0, 5, 1)
	input.Focus(nil)
	input.Draw(s)
	x, y, visible := s.GetCursor()
	if x != 4 || y != 0 || !visible {
		t.Fatalf("IME cursor: %d,%d %v", x, y, visible)
	}
	input.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), func(tview.Primitive) {})
	input.Draw(s)
	x, _, _ = s.GetCursor()
	if x != 3 {
		t.Fatalf("cursor after left: %d", x)
	}
	// Search inputs inside dropdowns receive delegated events while their Box
	// is blurred. Bubbles must still accept the paste and enforce the limit.
	input.Blur()
	input.PasteHandler()("你好", func(tview.Primitive) {})
	if input.GetText() != "中文ab你c" {
		t.Fatalf("bounded paste: %q", input.GetText())
	}
}

func TestDropdownSelectionAndCancel(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	next := NewInputField()
	selected := -1
	app := NewApplication().SetScreen(s)
	picker := NewDropDown().SetOptions([]string{"一", "二"}, func(_ string, n int) { selected = n; app.SetFocus(next) })
	app.SetRoot(picker, false)
	picker.SetRect(0, 0, 12, 1)
	picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), app.setFocus)
	picker.Draw(s)
	picker.list.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, 0), app.setFocus)
	picker.list.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), app.setFocus)
	if selected != 1 || picker.IsOpen() || app.GetFocus() != next {
		t.Fatal("selection lost callback focus or did not close")
	}
	app.SetFocus(picker)
	picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), app.setFocus)
	picker.list.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), app.setFocus)
	if picker.IsOpen() || selected != 1 || app.GetFocus() != picker {
		t.Fatal("cancel changed selection or focus")
	}
}

func TestTableAndTreeNavigation(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	grid := NewTable().SetFixed(1, 0).SetSelectable(true, false)
	grid.SetRect(0, 0, 8, 3)
	grid.SetCell(0, 0, NewTableCell("Name").SetSelectable(false))
	for n := 1; n <= 5; n++ {
		grid.SetCell(n, 0, NewTableCell("asset"))
	}
	grid.Select(1, 0)
	grid.Draw(s)
	grid.InputHandler()(tcell.NewEventKey(tcell.KeyEnd, 0, 0), func(tview.Primitive) {})
	grid.Draw(s)
	if row, col := grid.CellAt(0, 2); row != 5 || col != 0 {
		t.Fatalf("last visible table cell: %d,%d", row, col)
	}
	root := NewTreeNode("").SetSelectable(false).SetExpanded(true)
	parent := NewTreeNode("parent").SetExpanded(true)
	child := NewTreeNode("child")
	root.AddChild(parent.AddChild(child))
	tree := NewTreeView().SetRoot(root).SetTopLevel(1).SetCurrentNode(child)
	tree.SetRect(0, 4, 15, 3)
	if tree.GetRowCount() != 2 {
		t.Fatal("hidden root included")
	}
	parent.SetExpanded(false)
	tree.Draw(s)
	if tree.GetCurrentNode() != parent || tree.GetRowCount() != 1 {
		t.Fatal("collapsed tree retained hidden selection")
	}
}

func TestModalConfirmationAndTextScroll(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	chosen := -1
	modal := NewModal().SetText("Confirm").AddButtons([]string{"Cancel", "Close"}).SetDoneFunc(func(n int, _ string) { chosen = n })
	modal.SetRect(0, 0, 60, 20)
	modal.Focus(nil)
	modal.Draw(s)
	modal.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, 0), func(tview.Primitive) {})
	modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(tview.Primitive) {})
	if chosen != 1 {
		t.Fatalf("confirmation = %d", chosen)
	}
	view := NewTextView().SetDynamicColors(true).SetText("[red]第一行[-]\n第二行\n第三行")
	view.SetRect(0, 0, 12, 2)
	view.Draw(s)
	view.InputHandler()(tcell.NewEventKey(tcell.KeyEnd, 0, 0), func(tview.Primitive) {})
	row, _ := view.GetScrollOffset()
	if row != 1 {
		t.Fatalf("viewport offset = %d", row)
	}
	if plainTagged(tview.Escape("[red]")) != "[red]" {
		t.Fatal("escaped label became markup")
	}
}

func TestLayoutRoutesOnlyActivePage(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	base, dialog := NewInputField(), NewInputField()
	pages := NewPages().AddPage("base", base, true, true).AddPage("dialog", dialog, true, true)
	root := NewFlex().SetDirection(tview.FlexRow).AddItem(pages, 0, 1, true)
	app := NewApplication().SetScreen(s).SetRoot(root, true)
	app.draw()
	root.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'x', 0), app.setFocus)
	root.PasteHandler()("中文", app.setFocus)
	if dialog.GetText() != "x中文" || base.GetText() != "" {
		t.Fatal("overlay input leaked to the background page")
	}
	pages.RemovePage("dialog")
	app.SetFocus(base)
	app.draw()
	root.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'y', 0), app.setFocus)
	if base.GetText() != "y" {
		t.Fatal("background focus was not restored")
	}
}
