package handler

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

func (h *terminalUI) focusedContent() string {
	if len(h.dialogs) > 0 && h.dialogs[len(h.dialogs)-1].page == "full-text" {
		return ""
	}
	var fields []string
	field := func(label, value string) {
		label, value = strings.TrimSpace(label), strings.TrimSpace(value)
		if value == "" {
			value = h.tr("空", "Empty")
		}
		fields = append(fields, label+h.tr("：", ": ")+value)
	}
	optionLabel := h.tr("选项", "Option")
	if dropdown := h.focusedDropdown(); dropdown != nil {
		if dropdown == h.org {
			optionLabel = h.tr("组织", "Organization")
		} else if dropdown == h.treeKind {
			optionLabel = h.tr("树类型", "Tree view")
		} else if label := strings.TrimSpace(tuiPlainMnemonic(dropdown.GetLabel())); label != "" {
			optionLabel = label
		}
	}
	if h.modal {
		switch h.dialogs[len(h.dialogs)-1].page {
		case "language":
			optionLabel = h.tr("语言", "Language")
		case "appearance":
			optionLabel = h.tr("主题", "Theme")
		}
	}
	switch p := h.focusedControl().(type) {
	case *tview.List:
		if p.GetItemCount() > 0 {
			text, _ := p.GetItemText(p.GetCurrentItem())
			if h.modal && h.dialogs[len(h.dialogs)-1].page == "appearance" && strings.HasPrefix(text, "[#") {
				if _, name, ok := strings.Cut(text, "]●[-] "); ok {
					text = "● " + name
				}
			}
			field(optionLabel, text)
		}
	case *tview.TreeView:
		if n := p.GetCurrentNode(); n != nil {
			ref, ok := n.GetReference().(*tuiNodeRef)
			if !ok || ref.more {
				break
			}
			field(h.tr("节点名称", "Node name"), cleanTUIText(ref.scope.Label))
			path := ref.scope.Path
			if path == "" {
				path = ref.scope.Label
			}
			field(h.tr("路径", "Path"), "/"+cleanTUIText(strings.TrimLeft(path, "/")))
			if ref.count != nil {
				field(h.tr("资产数量", "Asset count"), fmt.Sprint(*ref.count))
			}
			if h.organizationsEnabled {
				field(h.tr("组织", "Organization"), cleanTUIText(ref.scope.Org.Name))
			}
		}
	case *tview.DropDown:
		if index, text := p.GetCurrentOption(); index >= 0 {
			field(optionLabel, text)
		}
	case *tview.Table:
		row, col := p.GetSelection()
		if p == h.sessionTabs {
			if col >= 0 && col < p.GetColumnCount() {
				field(h.tr("窗口", "Window"), h.sessionTabLabel(col-1))
			}
			break
		}
		if row < 0 || row >= p.GetRowCount() || p.GetCell(row, 0).NotSelectable {
			break
		}
		for col := 0; col < p.GetColumnCount(); col++ {
			label := strings.TrimSpace(p.GetCell(0, col).Text)
			if label == "" { // Hidden columns have no header or value.
				continue
			}
			if label == "#" {
				label = h.tr("序号", "No.")
			}
			cell := p.GetCell(row, col)
			value := cell.Text
			if fullValue, ok := cell.GetReference().(string); ok {
				value = cleanTUIText(fullValue)
			}
			field(label, value)
		}
	case *tview.InputField:
		label := strings.TrimSpace(tuiPlainMnemonic(p.GetLabel()))
		if p == h.search {
			label = h.tr("搜索", "Search")
		} else if label == "" {
			label = h.tr("内容", "Content")
		}
		field(label, cleanTUIText(p.GetText()))
	case *tview.Button:
		field(h.tr("操作", "Action"), p.GetLabel())
	}
	return tuiPlainMnemonic(strings.Join(fields, "\n\n"))
}

func (h *terminalUI) showFullText() {
	text := h.focusedContent()
	if text == "" {
		return
	}
	title := h.tr("完整内容", "Full text")
	width, height := 94, 24
	switch focused := h.focusedControl().(type) {
	case *tview.TreeView:
		title = h.tr("节点详情", "Node details")
		width = 68
		height = min(14, max(10, strings.Count(text, "\n")+8))
	case *tview.Table:
		if focused == h.table {
			title = h.tr("资产详情", "Asset details")
			width = 68
			height = min(18, max(10, strings.Count(text, "\n")+8))
		}
	}
	view := tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetWordWrap(true).
		SetTextStyle(tcell.StyleDefault.Foreground(tui.Foreground).Background(tui.Panel)).SetText(tview.Unescape(text))
	view.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEnter {
			h.dismissModal()
		}
	})
	close := tview.NewButton(h.tr("关闭", "Close") + " · Esc").
		SetStyle(tcell.StyleDefault.Foreground(tui.Muted).Background(tui.Panel)).
		SetActivatedStyle(tuiButtonFocusedStyle).
		SetSelectedFunc(h.dismissModal)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(close, 1, 0, false)
	content.Box = tview.NewBox()
	content.SetBackgroundColor(tui.Panel)
	tuiDialogBorder(content.Box, title)
	h.openDialog("full-text", &tuiOverlay{Box: tview.NewBox(), child: content, width: width, height: height}, []tview.Primitive{view, close})
}
