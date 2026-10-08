package handler

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"

	"github.com/jumpserver/koko/pkg/i18n"
)

type assetTUIDetailRow struct {
	key       string
	value     string
	separator bool
}

type assetTUIDetailDialog struct {
	title             string
	rows              []assetTUIDetailRow
	assetID           string
	loading           bool
	scroll            int
	scrollbarDragging bool
	scrollbarGrab     int
}

type assetTUIAssetDetailMsg struct {
	asset     model.PermAsset
	protocols []string
	err       error
}

func tuiIsSpaceKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeySpace ||
		msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == ' '
}

func newAssetTUIDetailDialog(title string, rows []assetTUIDetailRow) *assetTUIDetailDialog {
	for index := range rows {
		rows[index].value = strings.ReplaceAll(rows[index].value, "\r\n", "\n")
		rows[index].value = strings.ReplaceAll(rows[index].value, "\r", "\n")
		rows[index].value = strings.TrimSpace(strings.Map(func(value rune) rune {
			if value < ' ' && value != '\n' {
				return ' '
			}
			return value
		}, rows[index].value))
		if rows[index].value == "" {
			rows[index].value = "-"
		}
	}
	return &assetTUIDetailDialog{title: title, rows: rows}
}

func (m *assetTUI) openAssetDetail() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.assets) {
		return nil
	}
	asset := m.assets[m.cursor]
	m.detailDialog = m.assetDetailDialog(asset, nil)
	if m.handler == nil || m.handler.jmsService == nil || m.selector == nil || m.selector.user == nil {
		return nil
	}
	m.detailDialog.loading = true
	m.detailDialog.rows[len(m.detailDialog.rows)-1].value = m.tr("加载中…", "Loading…")
	m.status = m.tr("正在加载资产详情…", "Loading asset details…")
	return func() tea.Msg {
		client := m.handler.assetClient(asset.OrgID)
		detail, err := client.GetUserPermAssetDetailById(m.selector.user.ID, asset.ID)
		if err != nil {
			return assetTUIAssetDetailMsg{asset: asset, err: err}
		}
		protocols := make([]string, 0, len(detail.PermedProtocols))
		for _, protocol := range detail.PermedProtocols {
			if name := strings.TrimSpace(protocol.Name); name != "" {
				protocols = append(protocols, name)
			}
		}
		return assetTUIAssetDetailMsg{asset: asset, protocols: protocols}
	}
}

func (m *assetTUI) assetDetailDialog(asset model.PermAsset, protocols []string) *assetTUIDetailDialog {
	lang := i18n.NewLang(m.handler.i18nLang)
	active := lang.T("No")
	if asset.IsActive {
		active = lang.T("Yes")
	}
	dialog := newAssetTUIDetailDialog(asset.Name, []assetTUIDetailRow{
		{key: lang.T("ID"), value: asset.ID},
		{key: lang.T("Name"), value: asset.Name},
		{key: lang.T("Address"), value: asset.Address},
		{key: lang.T("Comment"), value: asset.Comment},
		{key: lang.T("Platform"), value: asset.Platform.Name},
		{key: lang.T("Category"), value: string(asset.Category)},
		{key: lang.T("Type"), value: string(asset.Type)},
		{key: lang.T("Organization"), value: asset.OrgName},
		{key: lang.T("Active"), value: active},
		{key: lang.T("Protocol"), value: strings.Join(protocols, " · ")},
	})
	dialog.assetID = asset.ID
	return dialog
}

func (m *assetTUI) openTreeNodeDetail(rows []assetTUITreeRow) {
	if m.treeDialog == nil || m.treeDialog.cursor < 0 || m.treeDialog.cursor >= len(rows) {
		return
	}
	cache := m.treeCache(m.treeDialog.kind)
	node := cache.nodes[rows[m.treeDialog.cursor].id]
	if node == nil {
		return
	}
	lang := i18n.NewLang(m.handler.i18nLang)
	m.detailDialog = newAssetTUIDetailDialog(node.name, []assetTUIDetailRow{
		{key: lang.T("ID"), value: node.id},
		{key: lang.T("Name"), value: node.name},
		{key: lang.T("Key"), value: node.key},
		{key: lang.T("Path"), value: cache.path(rows[m.treeDialog.cursor].id)},
		{key: lang.T("Asset count"), value: strconv.Itoa(node.count)},
	})
}

func (m *assetTUI) openAccountDetail() {
	if m.dialog == nil {
		return
	}
	account, ok := m.dialog.selectedAccount()
	if !ok {
		return
	}
	lang := i18n.NewLang(m.handler.i18nLang)
	actions := make([]string, 0, len(account.Actions))
	for _, action := range account.Actions {
		label := strings.TrimSpace(action.Label)
		if label == "" {
			label = strings.TrimSpace(action.Value)
		}
		if label != "" {
			actions = append(actions, label)
		}
	}
	m.detailDialog = newAssetTUIDetailDialog(account.Username, []assetTUIDetailRow{
		{key: lang.T("Name"), value: account.Name},
		{key: lang.T("Username"), value: account.Username},
		{key: lang.T("Actions"), value: strings.Join(actions, " · ")},
	})
}

func (m *assetTUI) detailDialogGeometry() assetTUIDialogGeometry {
	width := min(56, max(1, m.width-8))
	rows, _ := m.detailDialogRows(width)
	height := min(len(rows)+6, max(1, m.height-4))
	return assetTUIDialogGeometry{
		x: max(0, m.width-width-1), y: max(0, m.height-height-1),
		width: width, height: height, rows: max(1, height-6),
	}
}

func (m *assetTUI) detailDialogRows(width int) ([]assetTUIDetailRow, int) {
	keyWidth := 0
	for _, row := range m.detailDialog.rows {
		keyWidth = max(keyWidth, runewidth.StringWidth(row.key))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	valueWidth := max(1, width-keyWidth-8)
	rows := make([]assetTUIDetailRow, 0, max(0, len(m.detailDialog.rows)*2-1))
	for rowIndex, row := range m.detailDialog.rows {
		lines := strings.Split(runewidth.Wrap(row.value, valueWidth), "\n")
		for index, line := range lines {
			key := ""
			if index == 0 {
				key = row.key
			}
			rows = append(rows, assetTUIDetailRow{key: key, value: line})
		}
		if rowIndex+1 < len(m.detailDialog.rows) {
			rows = append(rows, assetTUIDetailRow{separator: true})
		}
	}
	return rows, keyWidth
}

func (m *assetTUI) updateDetailDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := m.detailDialog
	geometry := m.detailDialogGeometry()
	rows, _ := m.detailDialogRows(geometry.width)
	visible := max(1, geometry.rows)
	maxScroll := max(0, len(rows)-visible)
	switch msg.Type {
	case tea.KeyEsc:
		if dialog.loading {
			m.clearStatus()
		}
		m.detailDialog = nil
	case tea.KeyUp:
		dialog.scroll = max(0, dialog.scroll-1)
	case tea.KeyDown:
		dialog.scroll = min(maxScroll, dialog.scroll+1)
	case tea.KeyPgUp:
		dialog.scroll = max(0, dialog.scroll-visible)
	case tea.KeyPgDown:
		dialog.scroll = min(maxScroll, dialog.scroll+visible)
	case tea.KeyHome:
		dialog.scroll = 0
	case tea.KeyEnd:
		dialog.scroll = maxScroll
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == 'k' {
			dialog.scroll = max(0, dialog.scroll-1)
		} else if len(msg.Runes) == 1 && msg.Runes[0] == 'j' {
			dialog.scroll = min(maxScroll, dialog.scroll+1)
		}
	}
	return m, nil
}

func (m *assetTUI) updateDetailDialogMouse(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	dialog := m.detailDialog
	geometry := m.detailDialogGeometry()
	rows, _ := m.detailDialogRows(geometry.width)
	listY := geometry.y + 3
	if dialog.scrollbarDragging {
		if event.Action == tea.MouseActionRelease {
			dialog.scrollbarDragging = false
			return m, nil
		}
		if event.Action == tea.MouseActionMotion {
			row := max(0, min(event.Y-listY, geometry.rows-1))
			dialog.scroll = tuiScrollbarStartAt(row, dialog.scrollbarGrab, len(rows), geometry.rows)
		}
		return m, nil
	}
	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelUp {
		dialog.scroll = max(0, dialog.scroll-1)
		return m, nil
	}
	if event.Button == tea.MouseButtonWheelDown {
		dialog.scroll = min(max(0, len(rows)-geometry.rows), dialog.scroll+1)
		return m, nil
	}
	if event.Button == tea.MouseButtonLeft && len(rows) > geometry.rows &&
		event.X == geometry.x+geometry.width-2 && event.Y >= listY && event.Y < listY+geometry.rows {
		thumbStart, thumbSize, _, _ := tuiScrollbarMetrics(dialog.scroll, len(rows), geometry.rows)
		row := event.Y - listY
		dialog.scrollbarGrab = thumbSize / 2
		if row >= thumbStart && row < thumbStart+thumbSize {
			dialog.scrollbarGrab = row - thumbStart
		}
		dialog.scrollbarDragging = true
		dialog.scroll = tuiScrollbarStartAt(row, dialog.scrollbarGrab, len(rows), geometry.rows)
	}
	return m, nil
}

func (m *assetTUI) renderDetailDialog(lines []string) {
	geometry := m.detailDialogGeometry()
	if geometry.width < 24 || geometry.height < 7 || geometry.y < 0 || geometry.y+geometry.height > len(lines) {
		return
	}
	dialog := m.detailDialog
	rows, keyWidth := m.detailDialogRows(geometry.width)
	dialog.scroll = max(0, min(dialog.scroll, max(0, len(rows)-geometry.rows)))
	popup := tuiDialogFrame("", geometry.width, geometry.height)
	popup[1] = "│" + tuiCenter(dialog.title, geometry.width-2) + "│"
	popup[2] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	showScrollbar := len(rows) > geometry.rows
	valueWidth := max(1, geometry.width-keyWidth-8)
	for row := 0; row < geometry.rows; row++ {
		key, value := "", ""
		separator := false
		if position := dialog.scroll + row; position < len(rows) {
			key = rows[position].key
			value = rows[position].value
			separator = rows[position].separator
		}
		scrollbar := " "
		if showScrollbar {
			scrollbar = tuiScrollbarCell(row, dialog.scroll, len(rows), geometry.rows)
		}
		if separator {
			popup[row+3] = "│  " + strings.Repeat(" ", keyWidth) + " │ " +
				strings.Repeat(" ", valueWidth) + scrollbar + "│"
			continue
		}
		if key == "" && value == "" {
			popup[row+3] = "│" + strings.Repeat(" ", geometry.width-3) + scrollbar + "│"
			continue
		}
		popup[row+3] = "│  " + tuiFit(key, keyWidth) + " │ " + tuiFit(value, valueWidth) + scrollbar + "│"
	}
	lang := i18n.NewLang(m.handler.i18nLang)
	popup[geometry.height-3] = "├" + strings.Repeat("─", geometry.width-2) + "┤"
	popup[geometry.height-2] = "│" + tuiDialogLeft("esc:"+lang.T("Cancel"), geometry.width-2) + "│"
	m.overlayDialog(lines, popup, geometry)
}
