package handler

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jumpserver-dev/sdk-go/model"
)

func TestAssetTUISearchLineShowsInput(t *testing.T) {
	model := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		width:       20,
		searching:   true,
		searchInput: []rune("host"),
	}
	if line := model.searchLine(); !strings.Contains(line, "host") {
		t.Fatalf("search line does not contain input: %q", line)
	} else if strings.Contains(line, "█") {
		t.Fatalf("search line still contains a simulated cursor: %q", line)
	}
}

func TestAssetTUISearchUsesTerminalCursor(t *testing.T) {
	model := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		width:       20,
		height:      2,
		searching:   true,
		searchInput: []rune("host"),
	}
	var output bytes.Buffer
	writer := assetTUICursorWriter{output: &output, altScreen: true}
	if _, err := writer.Write([]byte(model.View())); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	if strings.Contains(rendered, tuiCursorMarkerPrefix) {
		t.Fatalf("cursor marker was written to the terminal: %q", rendered)
	}
	if !strings.HasSuffix(rendered, "\x1b[1;16H"+tuiShowCursor) {
		t.Fatalf("terminal cursor is not positioned after the search input: %q", rendered)
	}
}

func TestAssetTUISearchLineDefaultLabel(t *testing.T) {
	model := assetTUI{handler: &InteractiveHandler{i18nLang: "zh-CN"}, width: 20}
	if line := strings.TrimSpace(model.searchLine()); line != "我的资产" {
		t.Fatalf("unexpected default search label: %q", line)
	}
}

func TestAssetTUISearchLineClickFocusesSearch(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"},
		width:   80,
		query:   "host",
	}
	click := tea.MouseMsg{X: 10, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	tui.Update(click)
	if !tui.searching || string(tui.searchInput) != "host" {
		t.Fatalf("search was not focused with the current query: %q", tui.searchInput)
	}
}

func TestAssetTUISearchFooterOnlyShowsActiveShortcuts(t *testing.T) {
	tui := assetTUI{
		handler:   &InteractiveHandler{i18nLang: "en"},
		width:     160,
		searching: true,
		status:    "Type a keyword, Enter to search, Esc to cancel",
	}
	footer := tui.footerLine()
	for _, shortcut := range []string{"Enter Search", "Esc Cancel", "Backspace/Delete Delete", "Ctrl+U Clear"} {
		if !strings.Contains(footer, shortcut) {
			t.Fatalf("search footer does not contain %q: %q", shortcut, footer)
		}
	}
	for _, unavailable := range []string{"/ Search", "Text mode", "Select", "Page", "Ctrl+C"} {
		if strings.Contains(footer, unavailable) {
			t.Fatalf("search footer contains unavailable shortcut %q: %q", unavailable, footer)
		}
	}
	if _, cmd := tui.updateSearch(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil || !tui.searching {
		t.Fatal("Ctrl+C should not exit while search is focused")
	}
}

func TestAssetTUIVIKeyNavigation(t *testing.T) {
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		pageSize:    1,
		offset:      1,
		assets:      make([]model.PermAsset, 2),
		connectable: []bool{true, true},
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if tui.cursor != 1 {
		t.Fatalf("j did not move the selection down: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if tui.cursor != 0 {
		t.Fatalf("k did not move the selection up: %d", tui.cursor)
	}
	tui.hasNext = true
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}); cmd == nil {
		t.Fatal("l did not request the next page")
	}

	previous := assetTUI{
		handler:  &InteractiveHandler{i18nLang: "en"},
		selector: &UserSelectHandler{},
		pageSize: 1,
		offset:   1,
		hasPrev:  true,
	}
	if _, cmd := previous.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}); cmd == nil {
		t.Fatal("h did not request the previous page")
	}
	if footer := tui.footerLine(); strings.Contains(footer, "j/k") || strings.Contains(footer, "h/l") {
		t.Fatalf("vi keys should not be shown in the footer: %q", footer)
	}
}

func TestAssetTUIHidesPageLineWhenAllAssetsFit(t *testing.T) {
	assets := make([]model.PermAsset, 7)
	assets[0].Name = "server-1"
	assets[6].Name = "server-7"
	connectable := make([]bool, len(assets))
	for index := range connectable {
		connectable[index] = true
	}
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		width:       40,
		height:      10,
		total:       len(assets),
		assets:      assets,
		connectable: connectable,
	}
	lines := strings.Split(tui.View(), "\n")
	if !strings.HasPrefix(lines[1], tuiSelectedStyle) || !strings.HasPrefix(lines[2], tuiSelectedStyle) {
		t.Fatalf("header and selected asset styles differ: %q / %q", lines[1], lines[2])
	}
	if !strings.Contains(lines[8], "server-7") {
		t.Fatalf("last asset was not rendered in the page line space: %q", lines[8])
	}
	if line := tui.pageLine(); line != "" {
		t.Fatalf("page line is visible for a single page: %q", line)
	}
}

func TestAssetTUIPageLineFollowsLastAsset(t *testing.T) {
	assets := make([]model.PermAsset, 6)
	connectable := make([]bool, len(assets))
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		width:       40,
		height:      10,
		total:       8,
		assets:      assets,
		connectable: connectable,
		hasNext:     true,
	}
	lines := strings.Split(tui.View(), "\n")
	if !strings.Contains(lines[8], "1-6/8") {
		t.Fatalf("page line does not follow the last asset: %q", lines[8])
	}
}

func TestAssetTUIDoubleClickOpensConnectionDialog(t *testing.T) {
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		width:       80,
		height:      24,
		assets:      []model.PermAsset{{ID: "asset-1"}},
		connectable: []bool{true},
	}
	click := tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	if _, cmd := tui.Update(click); cmd != nil {
		t.Fatal("first click should only select the row")
	}
	if _, cmd := tui.Update(click); cmd == nil || !tui.loadingChoices {
		t.Fatal("second click should load account and protocol choices")
	}
}

func TestAssetTUIDialogCreatesConnection(t *testing.T) {
	tui := assetTUI{dialog: newAssetTUIDialog(
		model.PermAsset{ID: "asset-1"},
		[]model.PermAccount{{Alias: "account-1"}},
		[]string{"ssh"},
	)}
	if _, cmd := tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || tui.connection == nil {
		t.Fatal("dialog selection should create a TUI connection")
	}
}

func TestAssetTUIDialogVIKeyNavigation(t *testing.T) {
	tui := assetTUI{dialog: newAssetTUIDialog(
		model.PermAsset{},
		[]model.PermAccount{{Alias: "account-1"}, {Alias: "account-2"}},
		[]string{"ssh", "telnet"},
	)}
	for _, key := range []rune{'j', 'l'} {
		tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if tui.dialog.accountIndex != 1 || tui.dialog.protocolIndex != 1 {
		t.Fatalf("j/l did not move dialog selection: account=%d protocol=%d",
			tui.dialog.accountIndex, tui.dialog.protocolIndex)
	}
	for _, key := range []rune{'k', 'h'} {
		tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if tui.dialog.accountIndex != 0 || tui.dialog.protocolIndex != 0 {
		t.Fatalf("k/h did not move dialog selection: account=%d protocol=%d",
			tui.dialog.accountIndex, tui.dialog.protocolIndex)
	}
}

func TestAssetTUIDialogTitleAndAccountSearch(t *testing.T) {
	dialog := newAssetTUIDialog(
		model.PermAsset{Name: "server"},
		[]model.PermAccount{{Name: "Alice"}, {Name: "Bob"}},
		[]string{"ssh"},
	)
	dialog.accountSearch = []rune("bob")
	dialog.filterAccounts()
	account, ok := dialog.selectedAccount()
	if !ok || account.Name != "Bob" {
		t.Fatalf("unexpected filtered account: %#v", account)
	}
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 24, dialog: dialog}
	lines := make([]string, tui.height)
	tui.renderDialog(lines)
	geometry := tui.dialogGeometry()
	if strings.Contains(lines[geometry.y], "Connect - server") ||
		!strings.Contains(lines[geometry.y+1], "Connect - server") {
		t.Fatalf("dialog title is not inside the border: %q / %q", lines[geometry.y], lines[geometry.y+1])
	}
	if !strings.Contains(lines[geometry.y+1], "│  Connect - server") {
		t.Fatalf("dialog title is not left aligned: %q", lines[geometry.y+1])
	}
	if !strings.Contains(lines[geometry.y+2], "├─") {
		t.Fatalf("dialog title has no separator: %q", lines[geometry.y+2])
	}
	if !strings.Contains(lines[geometry.y+3], "│  "+tuiSelectedProtocolStyle+"ssh") {
		t.Fatalf("selected protocol is not styled and left aligned: %q", lines[geometry.y+3])
	}
	if strings.Trim(lines[geometry.y+4], " │") != "" {
		t.Fatalf("protocol and account have no blank line between them: %q", lines[geometry.y+4])
	}
	if !strings.Contains(lines[geometry.y+5], "│  Account: bob") {
		t.Fatalf("account search label is unexpected: %q", lines[geometry.y+5])
	}
	if !strings.Contains(lines[geometry.y+geometry.height-2], "│  ↑/↓ Account") {
		t.Fatalf("dialog shortcuts are not left aligned: %q", lines[geometry.y+geometry.height-2])
	}
	if hint := lines[geometry.y+geometry.height-2]; strings.Contains(hint, "j/k") || strings.Contains(hint, "h/l") {
		t.Fatalf("vi keys should not be shown in dialog shortcuts: %q", hint)
	}
	if strings.Trim(lines[geometry.y+geometry.height-3], " │") != "" {
		t.Fatalf("dialog shortcuts have no blank line above them: %q", lines[geometry.y+geometry.height-3])
	}
}

func TestAssetTUIDialogProtocolSelection(t *testing.T) {
	dialog := newAssetTUIDialog(
		model.PermAsset{Name: "server"},
		[]model.PermAccount{{Name: "Alice"}},
		[]string{"ssh", "telnet", "k8s"},
	)
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 24, dialog: dialog}
	geometry := tui.dialogGeometry()
	_, hits := tuiDialogProtocolLine(dialog.protocols, dialog.protocolIndex, geometry.width-4)
	click := tea.MouseMsg{
		X: geometry.x + 3 + hits[1].start, Y: geometry.y + 3,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	tui.Update(click)
	if dialog.protocolIndex != 1 {
		t.Fatalf("clicked protocol was not selected: %d", dialog.protocolIndex)
	}
	tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRight})
	if dialog.protocolIndex != 2 {
		t.Fatalf("right key did not select the next protocol: %d", dialog.protocolIndex)
	}
}

func TestAssetTUIDialogCoversCellsAroundBorder(t *testing.T) {
	dialog := newAssetTUIDialog(
		model.PermAsset{Name: "server"},
		[]model.PermAccount{{Name: "Alice"}},
		[]string{"ssh"},
	)
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 100, height: 24, dialog: dialog}
	lines := make([]string, tui.height)
	for i := range lines {
		lines[i] = strings.Repeat(".", tui.width)
	}
	tui.renderDialog(lines)
	geometry := tui.dialogGeometry()
	if !strings.Contains(lines[geometry.y+5], "│  Account") ||
		strings.Contains(lines[geometry.y+5], "/ Search") {
		t.Fatalf("default account label is unexpected: %q", lines[geometry.y+5])
	}
	maskX := geometry.x - 4
	maskRight := geometry.x + geometry.width + 4
	line := lines[geometry.y]
	if !strings.HasPrefix(line, strings.Repeat(".", maskX)) ||
		!strings.HasSuffix(line, strings.Repeat(".", tui.width-maskRight)) ||
		!strings.Contains(line, "    ┌") {
		t.Fatalf("dialog side mask has unexpected bounds: %q", line)
	}
	for _, row := range []int{geometry.y - 2, geometry.y - 1, geometry.y + geometry.height, geometry.y + geometry.height + 1} {
		masked := lines[row]
		if masked[maskX:maskRight] != strings.Repeat(" ", maskRight-maskX) {
			t.Fatalf("dialog row %d was not masked: %q", row, masked)
		}
	}
}
