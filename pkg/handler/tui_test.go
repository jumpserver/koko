package handler

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"
	terminal "golang.org/x/term"

	"github.com/jumpserver/koko/pkg/srvconn"
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

func TestAssetTUITopLineShowsProductNameAndUserName(t *testing.T) {
	setting := &model.PublicSetting{}
	setting.Interface.LoginTitle = "JumpServer"

	model := assetTUI{
		handler: &InteractiveHandler{
			i18nLang:      "en",
			user:          &model.User{Name: "Alice", Username: "hidden-login"},
			publicSetting: setting,
			coreVersion:   "v4.10.0",
		},
		width: 80,
	}
	line := model.searchLine()
	if !strings.HasSuffix(line, "Alice | JumpServer (v4.10.0)") {
		t.Fatalf("top-right product information is missing: %q", line)
	}
	if strings.Contains(line, "hidden-login") {
		t.Fatalf("top-right product information exposed the username: %q", line)
	}
	model.searching = true
	model.searchInput = []rune("host")
	if line = model.searchLine(); strings.Contains(line, "Alice") || strings.Contains(line, "JumpServer") {
		t.Fatalf("top-right information is visible while searching: %q", line)
	}
}

func TestAssetTUISearchLineClickFocusesSearch(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"},
		width:   80,
		query:   "host",
		status:  "Loaded",
	}
	click := tea.MouseMsg{X: 10, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	tui.Update(click)
	if !tui.searching || string(tui.searchInput) != "host" {
		t.Fatalf("search was not focused with the current query: %q", tui.searchInput)
	}
	if tui.status != "" {
		t.Fatalf("search focus left duplicate instructions in the status line: %q", tui.status)
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
	for _, shortcut := range []string{"enter:Search", "esc:Cancel", "backspace:Delete", "ctrl+u:Clear"} {
		if !strings.Contains(footer, shortcut) {
			t.Fatalf("search footer does not contain %q: %q", shortcut, footer)
		}
	}
	for _, unavailable := range []string{"/:Search", "Text mode", "Select", "Page", "ctrl+c"} {
		if strings.Contains(footer, unavailable) {
			t.Fatalf("search footer contains unavailable shortcut %q: %q", unavailable, footer)
		}
	}
	if _, cmd := tui.updateSearch(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil || !tui.searching {
		t.Fatal("Ctrl+C should not exit while search is focused")
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if tui.helpDialog || string(tui.searchInput) != "?" {
		t.Fatalf("question mark should remain searchable while search is focused: help=%v input=%q",
			tui.helpDialog, tui.searchInput)
	}
}

func TestAssetTUIStatusUsesDedicatedLineAndPersists(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 300, height: 12, status: "Saved",
	}
	if footer := tui.footerLine(); strings.Contains(footer, "Saved") {
		t.Fatalf("status is still shown in the shortcut footer: %q", footer)
	}
	lines := strings.Split(tui.View(), "\n")
	if strings.TrimSpace(lines[3]) != "" {
		t.Fatalf("status was rendered directly below the empty asset list: %q", lines[3])
	}
	if !strings.HasPrefix(lines[tui.height-2], tuiStatusPrefix+"Saved") {
		t.Fatalf("status is not fixed to the bottom: %q", lines[tui.height-2])
	}
	if !strings.Contains(lines[tui.height-1], "?:View help") {
		t.Fatalf("shortcuts are not fixed below the status line: %q", lines[tui.height-1])
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tui.status != "Saved" {
		t.Fatalf("status did not remain visible: %q", tui.status)
	}
}

func TestAssetTUIInitialLoadingKeepsStatusAndShortcutsAtBottom(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 80, height: 12, loading: true, status: "Loading assets…",
	}
	lines := strings.Split(tui.View(), "\n")
	if strings.TrimSpace(lines[3]) != "" {
		t.Fatalf("initial loading status was rendered below the empty asset list: %q", lines[3])
	}
	if !strings.HasPrefix(lines[tui.height-2], tuiStatusPrefix+"Loading assets…") {
		t.Fatalf("initial loading status is not at the bottom: %q", lines[tui.height-2])
	}
	if !strings.Contains(lines[tui.height-1], "?:View help") {
		t.Fatalf("initial loading shortcuts are not at the bottom: %q", lines[tui.height-1])
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
	tui.Update(tea.KeyMsg{Type: tea.KeyUp})
	if tui.cursor != len(tui.assets)-1 {
		t.Fatalf("up did not wrap from the first asset to the last: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tui.cursor != 0 {
		t.Fatalf("down did not wrap from the last asset to the first: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if tui.cursor != len(tui.assets)-1 {
		t.Fatalf("k did not wrap from the first asset to the last: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if tui.cursor != 0 {
		t.Fatalf("j did not wrap from the last asset to the first: %d", tui.cursor)
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
	if footer := tui.footerLine(); strings.Contains(footer, "j, k") || strings.Contains(footer, "h, l") {
		t.Fatalf("vi keys should not be shown in the footer: %q", footer)
	}
}

func TestAssetTUIRefreshKeepsCurrentScope(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en"}
	selector := &UserSelectHandler{
		h: handler, currentType: TypeTypeAsset, selectedPath: "/Database/MySQL",
		loadingPolicy: loadingFromRemote,
	}
	tui := assetTUI{
		handler: handler, selector: selector, pageSize: 10, offset: 20, cursor: 3,
		query: "server", selectedTree: assetTUITypeTree,
		selectedTreeID: "mysql", selectedPath: "/Database/MySQL",
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); cmd == nil {
		t.Fatal("r did not request an asset refresh")
	}
	if !tui.loading || tui.offset != 20 || tui.cursor != 3 || tui.query != "server" ||
		tui.selectedTree != assetTUITypeTree || tui.selectedTreeID != "mysql" ||
		tui.selectedPath != "/Database/MySQL" || selector.currentType != TypeTypeAsset ||
		selector.selectedPath != "/Database/MySQL" {
		t.Fatalf("refresh changed the current asset scope: tui=%#v selector=%#v", tui, selector)
	}

	tui.loading = false
	tui.searching = true
	tui.searchInput = nil
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); cmd != nil ||
		string(tui.searchInput) != "r" {
		t.Fatalf("r did not remain searchable while search was focused: %q", tui.searchInput)
	}
}

func TestAssetTUIPagingShortcutsUseOffsetAndTotal(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		pageSize: 2, offset: 2, total: 6, assets: make([]model.PermAsset, 2),
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}); cmd == nil {
		t.Fatal("h did not request the previous page without an API previous link")
	}
	tui.loading = false
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}); cmd == nil {
		t.Fatal("l did not request the next page without an API next link")
	}
	tui.loading = false
	for _, key := range []rune{'p', 'P', 'n', 'N'} {
		if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}); cmd != nil || tui.loading {
			t.Fatalf("%c should not be a paging shortcut", key)
		}
	}
}

func TestAssetTUILanguageDialogKeyboard(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en"}
	tui := assetTUI{handler: handler, width: 200, height: 24}
	if footer := tui.footerLine(); !strings.Contains(footer, "?:View help") {
		t.Fatalf("help shortcut is missing from the footer: %q", footer)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if tui.languageDialog == nil || tui.languageDialog.index != 0 {
		t.Fatalf("language dialog did not select the current language: %#v", tui.languageDialog)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyDown})
	tui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tui.languageDialog != nil || handler.i18nLang != "zh" {
		t.Fatalf("language was not selected: dialog=%#v language=%q", tui.languageDialog, handler.i18nLang)
	}

	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	tui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tui.languageDialog != nil || handler.i18nLang != "zh" {
		t.Fatalf("Esc did not cancel language selection: dialog=%#v language=%q", tui.languageDialog, handler.i18nLang)
	}
}

func TestAssetTUIHelpDialogKeyboardAndRendering(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 80, height: 24, query: "host",
		selectedTree: assetTUITypeTree, selectedTreeID: "host",
	}
	footer := strings.TrimSpace(tui.footerLine())
	for _, shortcut := range []string{"/:Search", "enter:Connect", "space:Details", "g:Asset tree", "q:Quit", "?:View help"} {
		if !strings.Contains(footer, shortcut) {
			t.Fatalf("main footer is missing common shortcut %q: %q", shortcut, footer)
		}
	}
	for _, shortcut := range []string{"↑, ↓", "←, →", "j, k", "h, l", "s:Switch language"} {
		if strings.Contains(footer, shortcut) {
			t.Fatalf("main footer contains help-only shortcut %q: %q", shortcut, footer)
		}
	}
	tui.width = 24
	compactFooter := tui.footerLine()
	if runewidth.StringWidth(compactFooter) != tui.width || !strings.Contains(compactFooter, "?:View help") {
		t.Fatalf("footer did not adapt to a narrow terminal: %q", compactFooter)
	}
	tui.width = 80
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !tui.helpDialog {
		t.Fatal("question mark did not open the help dialog")
	}
	rows := tui.helpShortcutRows()
	shortcuts := make(map[string]string, len(rows))
	for _, row := range rows {
		shortcuts[row.key] = row.description
	}
	for key, description := range map[string]string{
		"↑, ↓, j, k": "Move the selection up or down",
		"←, →, h, l": "Go to the previous or next page",
		"ctrl+c, q":  "Quit",
		"x":          "Clear the current asset search",
		"c":          "Clear the selected tree node",
		"r":          "Refresh the current asset list and keep search and tree filters",
	} {
		if shortcuts[key] != description {
			t.Fatalf("help dialog is missing %q: %q: %#v", key, description, shortcuts)
		}
	}
	lines := strings.Split(tui.View(), "\n")
	geometry := tui.helpDialogGeometry(rows)
	if geometry.width != 64 || geometry.x+geometry.width != tui.width-1 ||
		geometry.y+geometry.height != tui.height-1 {
		t.Fatalf("help dialog is not aligned to the bottom right: %#v", geometry)
	}
	if strings.Contains(lines[geometry.y], "View help") ||
		!strings.Contains(lines[geometry.y+1], "View help") {
		t.Fatalf("help title is not centered inside the border: %q / %q",
			lines[geometry.y], lines[geometry.y+1])
	}
	content := strings.Join(lines[geometry.y+3:geometry.y+3+geometry.rows], "\n")
	if !strings.Contains(content, "j, k") || !strings.Contains(content, " │ ") {
		t.Fatalf("help shortcuts are not rendered in two columns: %q", content)
	}
	separatorLine := lines[geometry.y+4]
	if !strings.Contains(separatorLine, " │ ") || strings.ContainsAny(separatorLine, "├┼┤") {
		t.Fatalf("help shortcut spacing does not preserve only the vertical separator: %q", separatorLine)
	}
	protocolStart := geometry.y + 3 + geometry.rows
	if !strings.Contains(lines[protocolStart], "├") {
		t.Fatalf("supported protocols have no separator: %q", lines[protocolStart])
	}
	protocolContent := strings.Join(lines[protocolStart+1:geometry.y+geometry.height-1], "\n")
	if !strings.Contains(lines[protocolStart+1], "Protocols supported by the current terminal:") {
		t.Fatalf("supported protocol title is missing: %q", protocolContent)
	}
	if protocols := srvconn.SupportedProtocols(); len(protocols) > 0 &&
		strings.Contains(lines[protocolStart+1], protocols[0]) {
		t.Fatalf("supported protocols start on the title row: %q", lines[protocolStart+1])
	}
	protocolList := strings.Join(lines[protocolStart+2:geometry.y+geometry.height-1], "\n")
	for _, protocol := range srvconn.SupportedProtocols() {
		if !strings.Contains(protocolList, protocol) {
			t.Fatalf("supported protocol %q is missing: %q", protocol, protocolList)
		}
	}
	for row := protocolStart + 1; row < geometry.y+geometry.height-1; row++ {
		dialogLine := runewidth.TruncateLeft(lines[row], geometry.x, "")
		dialogLine = runewidth.Truncate(dialogLine, geometry.width, "")
		if !strings.HasSuffix(dialogLine, "│") {
			t.Fatalf("supported protocol row has no right border: %q", lines[row])
		}
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if tui.helpScroll == 0 {
		t.Fatal("help shortcut list did not scroll to the end")
	}
	scrolled := strings.Join(strings.Split(tui.View(), "\n")[geometry.y+3:geometry.y+3+geometry.rows], "\n")
	if !strings.Contains(scrolled, "esc") {
		t.Fatalf("last help shortcut is not reachable by scrolling: %q", scrolled)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tui.helpDialog {
		t.Fatal("Esc did not close the help dialog")
	}
}

func TestAssetTUILanguageDialogMouse(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en"}
	tui := assetTUI{handler: handler, width: 80, height: 24}
	tui.openLanguageDialog()
	geometry := tui.languageDialogGeometry()
	click := tea.MouseMsg{
		X: geometry.x + 4, Y: geometry.y + 4,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	tui.Update(click)
	tui.Update(click)
	if tui.languageDialog != nil || handler.i18nLang != "zh" {
		t.Fatalf("double-click did not select the language: dialog=%#v language=%q", tui.languageDialog, handler.i18nLang)
	}

	tui.openLanguageDialog()
	tui.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if tui.languageDialog != nil {
		t.Fatal("clicking outside did not close the language dialog")
	}
}

func TestAssetTUILanguageDialogRendering(t *testing.T) {
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 24}
	tui.openLanguageDialog()
	lines := make([]string, tui.height)
	for i := range lines {
		lines[i] = strings.Repeat(".", tui.width)
	}
	tui.renderLanguageDialog(lines)
	geometry := tui.languageDialogGeometry()
	if strings.Contains(lines[geometry.y], "language switch") ||
		!strings.Contains(lines[geometry.y+1], "│"+tuiCenter("language switch", geometry.width-2)+"│") {
		t.Fatalf("language dialog title is not centered inside the border: %q / %q",
			lines[geometry.y], lines[geometry.y+1])
	}
	if !strings.Contains(lines[geometry.y+3], tuiSelectedStyle) ||
		!strings.Contains(lines[geometry.y+3], "English") {
		t.Fatalf("current language is not selected: %q", lines[geometry.y+3])
	}
	if !strings.Contains(lines[geometry.y+geometry.height-3], "├") ||
		!strings.Contains(lines[geometry.y+geometry.height-3], "┤") {
		t.Fatalf("language shortcuts have no separator above them: %q", lines[geometry.y+geometry.height-3])
	}
}

func TestAssetTUITreeSingleRootExpandsFirstLevel(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en"}
	selector := &UserSelectHandler{h: handler, user: &model.User{ID: "user-1"}}
	tui := assetTUI{
		handler: handler, selector: selector,
		trees: map[assetTUITreeKind]*assetTUITreeCache{
			assetTUIAuthorizationTree: {
				kind: assetTUIAuthorizationTree, generation: 1,
				nodes: make(map[string]*assetTUITreeNode),
			},
		},
		treeDialog: &assetTUITreeDialog{kind: assetTUIAuthorizationTree},
	}
	cmd := tui.updateTreeNodes(assetTUITreeNodesMsg{
		kind: assetTUIAuthorizationTree, generation: 1,
		nodes: []assetTUITreeNode{{
			identity: "1", id: "node-1", key: "1", name: "Root", hasChildren: true,
		}},
	})
	root := tui.treeCache(assetTUIAuthorizationTree).nodes["1"]
	if !root.expanded || !root.loading || cmd == nil {
		t.Fatalf("single root did not start loading its first level: %#v", root)
	}
	tui.updateTreeNodes(assetTUITreeNodesMsg{
		kind: assetTUIAuthorizationTree, generation: 1, parent: "1",
		nodes: []assetTUITreeNode{{
			identity: "1:1", id: "node-2", key: "1:1", parent: "1", name: "Child",
			hasChildren: true,
		}},
	})
	if rows := tui.treeCache(assetTUIAuthorizationTree).visibleRows(); len(rows) != 2 {
		t.Fatalf("loaded first-level nodes are not visible: %#v", rows)
	}
}

func TestAssetTUITreeMultipleRootsStayCollapsed(t *testing.T) {
	cache := &assetTUITreeCache{
		kind: assetTUIAuthorizationTree, generation: 1,
		nodes: make(map[string]*assetTUITreeNode),
	}
	tui := assetTUI{
		handler:  &InteractiveHandler{i18nLang: "en"},
		selector: &UserSelectHandler{user: &model.User{ID: "user-1"}},
		trees:    map[assetTUITreeKind]*assetTUITreeCache{assetTUIAuthorizationTree: cache},
	}
	tui.updateTreeNodes(assetTUITreeNodesMsg{
		kind: assetTUIAuthorizationTree, generation: 1,
		nodes: []assetTUITreeNode{
			{identity: "1", id: "node-1", key: "1", name: "Org 1", hasChildren: true},
			{identity: "2", id: "node-2", key: "2", name: "Org 2", hasChildren: true},
		},
	})
	if cache.nodes["1"].expanded || cache.nodes["2"].expanded {
		t.Fatal("multiple roots should not be expanded automatically")
	}
}

func TestAssetTUITreeRestoresLastDialogState(t *testing.T) {
	cache := &assetTUITreeCache{
		kind: assetTUITypeTree, initialized: true,
		nodes: make(map[string]*assetTUITreeNode),
	}
	for index := 0; index < 10; index++ {
		id := fmt.Sprintf("type-%d", index)
		cache.roots = append(cache.roots, id)
		cache.nodes[id] = &assetTUITreeNode{identity: id, id: id, name: id}
	}
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 80, height: 18,
		trees:      map[assetTUITreeKind]*assetTUITreeCache{assetTUITypeTree: cache},
		treeDialog: &assetTUITreeDialog{kind: assetTUITypeTree, cursor: 6, scroll: 4},
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tui.treeDialog != nil || tui.lastTreeDialog != assetTUITypeTree {
		t.Fatalf("tree dialog state was not saved when closing: dialog=%#v kind=%d",
			tui.treeDialog, tui.lastTreeDialog)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if tui.treeDialog == nil || tui.treeDialog.kind != assetTUITypeTree ||
		tui.treeDialog.cursor != 6 || tui.treeDialog.scroll != 4 {
		t.Fatalf("tree dialog state was not restored: %#v", tui.treeDialog)
	}
}

func TestAssetTUITreeCacheAndScopeLabel(t *testing.T) {
	var screen bytes.Buffer
	handler := &InteractiveHandler{i18nLang: "en"}
	handler.term = terminal.NewTerminal(&screen, "")
	selector := &UserSelectHandler{h: handler}
	cache := &assetTUITreeCache{
		kind: assetTUITypeTree, initialized: true,
		nodes: map[string]*assetTUITreeNode{
			"ROOT": {identity: "ROOT", name: "All types"},
		},
		roots: []string{"ROOT"},
	}
	tui := assetTUI{
		handler: handler, selector: selector,
		trees: map[assetTUITreeKind]*assetTUITreeCache{
			assetTUIAuthorizationTree: {kind: assetTUIAuthorizationTree, initialized: true},
			assetTUITypeTree:          cache,
			assetTUIFavoriteTree:      {kind: assetTUIFavoriteTree, initialized: true},
		},
		selectedTree: assetTUITypeTree, selectedPath: "/All types", width: 80,
	}
	if _, cmd := tui.openTreeDialog(assetTUITypeTree); cmd != nil {
		t.Fatal("reopening an initialized tree should use the session cache")
	}
	label := strings.TrimSpace(tui.searchLine())
	if !strings.Contains(label, "My Assets · Type tree:/All types") {
		t.Fatalf("tree scope is missing from the asset header: %q", label)
	}
	if _, cmd := tui.updateTreeDialogKey(tea.KeyMsg{Type: tea.KeyTab}); cmd != nil ||
		tui.treeDialog.kind != assetTUIFavoriteTree {
		t.Fatalf("Tab did not switch to the favorite tree: dialog=%#v", tui.treeDialog)
	}
	if _, cmd := tui.updateTreeDialogKey(tea.KeyMsg{Type: tea.KeyTab}); cmd != nil ||
		tui.treeDialog.kind != assetTUIAuthorizationTree {
		t.Fatalf("Tab did not cycle back to the authorization tree: dialog=%#v", tui.treeDialog)
	}
	if _, cmd := tui.updateTreeDialogKey(tea.KeyMsg{Type: tea.KeyShiftTab}); cmd != nil ||
		tui.treeDialog.kind != assetTUIFavoriteTree {
		t.Fatalf("Shift+Tab did not cycle back to the favorite tree: dialog=%#v", tui.treeDialog)
	}
	if _, cmd := tui.updateTreeDialogKey(tea.KeyMsg{Type: tea.KeyTab}); cmd != nil ||
		tui.treeDialog.kind != assetTUIAuthorizationTree {
		t.Fatalf("Tab did not restore the authorization tree: dialog=%#v", tui.treeDialog)
	}
	tui.width, tui.height = 80, 24
	lines := make([]string, tui.height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", tui.width)
	}
	tui.renderTreeDialog(lines)
	geometry := tui.treeDialogGeometry()
	if geometry.width != 70 || geometry.height != 16 || geometry.x != (tui.width-geometry.width)/2 ||
		geometry.y != (tui.height-geometry.height)/2 {
		t.Fatalf("tree dialog does not have the expected centered dimensions: %#v", geometry)
	}
	titleLine := lines[geometry.y+1]
	for _, title := range []string{"Authorization tree", "Type tree", "Favorite tree"} {
		if !strings.Contains(titleLine, title) {
			t.Fatalf("tree title bar is missing %q: %q", title, titleLine)
		}
	}
	if !strings.Contains(titleLine, tuiSelectedStyle+"  Authorization tree  "+tuiStyleReset) {
		t.Fatalf("current tree title is not selected: %q", titleLine)
	}
	plainTitle := strings.ReplaceAll(strings.ReplaceAll(titleLine, tuiSelectedStyle, ""), tuiStyleReset, "")
	titleCells := strings.Split(strings.Trim(strings.TrimSpace(plainTitle), "│"), "│")
	if len(titleCells) != 3 || len(titleCells[0])-len(titleCells[2]) > 1 {
		t.Fatalf("tree titles are not split into three even cells: %q", titleLine)
	}
	shortcutLine := lines[geometry.y+geometry.height-2]
	if !strings.Contains(lines[geometry.y+geometry.height-3], "├") ||
		!strings.Contains(lines[geometry.y+geometry.height-3], "┤") {
		t.Fatalf("tree shortcuts have no separator above them: %q", lines[geometry.y+geometry.height-3])
	}
	for _, shortcut := range []string{"space:Details", "tab:Asset tree", "?:View help", "esc:Cancel"} {
		if !strings.Contains(shortcutLine, shortcut) {
			t.Fatalf("tree shortcuts do not fit on one line: %q", shortcutLine)
		}
	}
	tui.width = 30
	tui.searching = true
	tui.selectedPath = "/All types/Database/PostgreSQL"
	tui.searchInput = []rune("server")
	if line := tui.searchLine(); !strings.Contains(line, ": server") {
		t.Fatalf("scoped search input is not visible after the path: %q", line)
	}
	tui.searching = false
	tui.query = "server"
	tui.width = 500
	footer := tui.footerLine()
	for _, shortcut := range []string{"/:Search", "x:Clear search", "enter:Connect", "space:Details", "g:Asset tree", "c:Clear node", "t:Text mode", "q:Quit", "?:View help"} {
		if !strings.Contains(footer, shortcut) {
			t.Fatalf("asset footer is missing common shortcut %q: %q", shortcut, footer)
		}
	}
	for _, shortcut := range []string{"↑, ↓", "←, →", "j, k", "h, l", "s:Switch language"} {
		if strings.Contains(footer, shortcut) {
			t.Fatalf("asset footer contains help-only shortcut %q: %q", shortcut, footer)
		}
	}
	tui.treeDialog = nil
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}); cmd == nil ||
		tui.query != "" || tui.selectedTree != assetTUITypeTree {
		t.Fatalf("clear search shortcut did not preserve the tree scope: query=%q tree=%d", tui.query, tui.selectedTree)
	}
	tui.loading = false
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}); cmd == nil ||
		tui.selectedTree != 0 || tui.selectedTreeID != "" || tui.selectedPath != "" ||
		handler.treeSelected || selector.currentType != TypeAsset {
		t.Fatalf("clear node shortcut did not reset the tree scope: tui=%#v selector=%#v", tui, selector)
	}
}

func TestAssetTUITreeMouseSelectionKeepsRowAndArrowExpands(t *testing.T) {
	cache := &assetTUITreeCache{
		kind: assetTUIAuthorizationTree, initialized: true,
		nodes: make(map[string]*assetTUITreeNode),
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("node-%d", i)
		cache.roots = append(cache.roots, id)
		cache.nodes[id] = &assetTUITreeNode{
			identity: id, id: id, key: id, name: id,
			hasChildren: true, childrenLoaded: true,
		}
	}
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 18,
		trees: map[assetTUITreeKind]*assetTUITreeCache{
			assetTUIAuthorizationTree: cache,
			assetTUITypeTree: {
				kind: assetTUITypeTree, initialized: true, nodes: make(map[string]*assetTUITreeNode),
			},
		},
		treeDialog: &assetTUITreeDialog{
			kind: assetTUIAuthorizationTree, cursor: 2, scroll: 2, lastClickRow: -1,
		},
	}
	geometry := tui.treeDialogGeometry()
	click := tea.MouseMsg{
		X: geometry.x + 12, Y: geometry.y + 3 + 2,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	tui.Update(click)
	if tui.treeDialog.cursor != 4 || tui.treeDialog.scroll != 2 {
		t.Fatalf("single click moved the selected row: cursor=%d scroll=%d",
			tui.treeDialog.cursor, tui.treeDialog.scroll)
	}
	lines := make([]string, tui.height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", tui.width)
	}
	tui.renderTreeDialog(lines)
	selectedLine := lines[geometry.y+3+2]
	if strings.Contains(selectedLine, "▶") || !strings.Contains(selectedLine, tuiSelectedStyle) {
		t.Fatalf("selected tree row has an icon or no selected style: %q", selectedLine)
	}
	list := strings.Join(lines[geometry.y+3:geometry.y+3+geometry.rows], "\n")
	if !strings.Contains(list, "█") || !strings.Contains(list, "░") {
		t.Fatalf("long tree does not show a scrollbar: %q", list)
	}
	arrowClick := click
	arrowClick.X = geometry.x + 5
	tui.Update(arrowClick)
	if !cache.nodes["node-4"].expanded {
		t.Fatal("clicking the arrow area did not expand the node")
	}
	cache.nodes["node-4"].expanded = false
	leftClick := click
	leftClick.X = geometry.x + 1
	tui.Update(leftClick)
	if !cache.nodes["node-4"].expanded {
		t.Fatal("clicking to the left of the arrow did not expand the node")
	}
	scrollbarX := geometry.x + geometry.width - 2
	listY := geometry.y + 3
	thumbStart, _, _, _ := tuiScrollbarMetrics(tui.treeDialog.scroll, len(cache.roots), geometry.rows)
	tui.Update(tea.MouseMsg{
		X: scrollbarX, Y: listY + thumbStart,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !tui.treeDialog.scrollbarDragging {
		t.Fatal("tree scrollbar drag did not start")
	}
	tui.Update(tea.MouseMsg{
		X: scrollbarX, Y: listY + geometry.rows - 1,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion,
	})
	wantStart := len(cache.roots) - geometry.rows
	if tui.treeDialog.scroll != wantStart || tui.treeDialog.cursor != wantStart {
		t.Fatalf("tree scrollbar did not drag to the bottom: scroll=%d cursor=%d", tui.treeDialog.scroll, tui.treeDialog.cursor)
	}
	tui.Update(tea.MouseMsg{X: scrollbarX, Y: listY, Action: tea.MouseActionRelease})
	if tui.treeDialog.scrollbarDragging {
		t.Fatal("tree scrollbar drag did not stop")
	}
	titleClick := tea.MouseMsg{
		X: geometry.x + geometry.width/2, Y: geometry.y + 1,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	tui.Update(titleClick)
	if tui.treeDialog.kind != assetTUITypeTree {
		t.Fatalf("clicking the tree title did not switch tabs: %d", tui.treeDialog.kind)
	}
	lines = make([]string, tui.height)
	for index := range lines {
		lines[index] = strings.Repeat(" ", tui.width)
	}
	tui.renderTreeDialog(lines)
	if !strings.Contains(lines[geometry.y+geometry.height-2], "tab:Asset tree") {
		t.Fatalf("tree shortcut does not show Tab switching: %q", lines[geometry.y+geometry.height-2])
	}
}

func TestAssetTUITreeMouseDoubleClickChoosesNode(t *testing.T) {
	var screen bytes.Buffer
	handler := &InteractiveHandler{i18nLang: "en"}
	handler.term = terminal.NewTerminal(&screen, "")
	selector := &UserSelectHandler{h: handler, user: &model.User{ID: "user-1"}}
	cache := &assetTUITreeCache{
		kind: assetTUIAuthorizationTree, initialized: true,
		roots: []string{"root"},
		nodes: map[string]*assetTUITreeNode{
			"root": {identity: "root", id: "node-1", key: "root", name: "Root"},
		},
	}
	tui := assetTUI{
		handler: handler, selector: selector, width: 80, height: 18, pageSize: 5,
		trees:      map[assetTUITreeKind]*assetTUITreeCache{assetTUIAuthorizationTree: cache},
		treeDialog: &assetTUITreeDialog{kind: assetTUIAuthorizationTree, lastClickRow: -1},
	}
	geometry := tui.treeDialogGeometry()
	click := tea.MouseMsg{
		X: geometry.x + 12, Y: geometry.y + 3,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}
	if _, cmd := tui.Update(click); cmd != nil {
		t.Fatal("first click should only select the tree node")
	}
	if _, cmd := tui.Update(click); cmd == nil || tui.treeDialog != nil ||
		selector.currentType != TypeNodeAsset {
		t.Fatalf("double-click did not choose the tree node: dialog=%#v type=%d",
			tui.treeDialog, selector.currentType)
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
		height:      12,
		total:       len(assets),
		assets:      assets,
		connectable: connectable,
	}
	lines := strings.Split(tui.View(), "\n")
	if strings.TrimSpace(lines[1]) != strings.Repeat("─", tui.width) {
		t.Fatalf("asset title separator is missing: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], tuiBoldStyle) || strings.HasPrefix(lines[2], tuiSelectedStyle) ||
		!strings.HasPrefix(lines[3], tuiSelectedStyle) {
		t.Fatalf("header should be bold and selected asset highlighted: %q / %q", lines[2], lines[3])
	}
	if !strings.Contains(lines[9], "server-7") {
		t.Fatalf("last asset was not rendered in the page line space: %q", lines[9])
	}
	if line := tui.pageLine(); line != "" {
		t.Fatalf("page line is visible for a single page: %q", line)
	}
	if strings.TrimSpace(lines[10]) == strings.Repeat("─", tui.width) {
		t.Fatalf("separator is visible without pagination: %q", lines[10])
	}
}

func TestAssetTUIPageLineFollowsLastAsset(t *testing.T) {
	assets := make([]model.PermAsset, 5)
	connectable := make([]bool, len(assets))
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		width:       40,
		height:      11,
		total:       8,
		assets:      assets,
		connectable: connectable,
		hasNext:     true,
		status:      "Loaded",
	}
	lines := strings.Split(tui.View(), "\n")
	if strings.TrimSpace(lines[8]) != strings.Repeat("─", tui.width) {
		t.Fatalf("separator does not follow the last asset: %q", lines[8])
	}
	if !strings.HasPrefix(lines[9], tuiStatusPrefix+"Loaded") ||
		!strings.HasSuffix(lines[9], "1-5/8") {
		t.Fatalf("status and page line does not follow the separator: %q", lines[9])
	}
	if !strings.Contains(lines[10], "?:View help") {
		t.Fatalf("shortcut line does not follow the status and page line: %q", lines[10])
	}
}

func TestAssetTUITypeCategoryDoesNotBecomeAssetType(t *testing.T) {
	category, assetType, err := normalizeAssetTUITypeFilter("category", "", "host")
	if err != nil {
		t.Fatal(err)
	}
	if category != "host" || assetType != "" {
		t.Fatalf("category node became an asset type: category=%q type=%q", category, assetType)
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
	click := tea.MouseMsg{X: 1, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
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
	if !strings.Contains(lines[geometry.y+1], "│"+tuiCenter("Connect - server", geometry.width-2)+"│") {
		t.Fatalf("dialog title is not centered: %q", lines[geometry.y+1])
	}
	if !strings.Contains(lines[geometry.y+2], "├─") {
		t.Fatalf("dialog title has no separator: %q", lines[geometry.y+2])
	}
	if !strings.Contains(lines[geometry.y+3], "│  Protocol") {
		t.Fatalf("protocol label is not left aligned: %q", lines[geometry.y+3])
	}
	if !strings.Contains(lines[geometry.y+4], "│    "+tuiSelectedProtocolStyle+"ssh") {
		t.Fatalf("selected protocol is not styled and indented: %q", lines[geometry.y+4])
	}
	if strings.Trim(lines[geometry.y+5], " │") != "" {
		t.Fatalf("protocol and account have no blank line between them: %q", lines[geometry.y+5])
	}
	if !strings.Contains(lines[geometry.y+6], "│  Account: bob") {
		t.Fatalf("account search label is unexpected: %q", lines[geometry.y+6])
	}
	if !strings.Contains(lines[geometry.y+7], "│    "+tuiSelectedStyle) ||
		strings.Contains(lines[geometry.y+7], "▶") {
		t.Fatalf("selected account is not styled without an arrow: %q", lines[geometry.y+7])
	}
	if !strings.Contains(lines[geometry.y+geometry.height-2], "│  /:Search · space:Details · enter:Connect · esc:Cancel · ?:View help") {
		t.Fatalf("dialog shortcuts are not left aligned: %q", lines[geometry.y+geometry.height-2])
	}
	if hint := lines[geometry.y+geometry.height-2]; strings.Contains(hint, "j, k") || strings.Contains(hint, "h, l") {
		t.Fatalf("vi keys should not be shown in dialog shortcuts: %q", hint)
	}
	if !strings.Contains(lines[geometry.y+geometry.height-3], "├") ||
		!strings.Contains(lines[geometry.y+geometry.height-3], "┤") {
		t.Fatalf("dialog shortcuts have no separator above them: %q", lines[geometry.y+geometry.height-3])
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
	_, hits := tuiDialogProtocolLine(dialog.protocols, dialog.protocolIndex, geometry.width-6)
	click := tea.MouseMsg{
		X: geometry.x + 5 + hits[1].start, Y: geometry.y + 4,
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

func TestAssetTUIDialogShowsAccountScrollbar(t *testing.T) {
	accounts := make([]model.PermAccount, 12)
	for index := range accounts {
		accounts[index].Name = fmt.Sprintf("account-%d", index)
	}
	dialog := newAssetTUIDialog(model.PermAsset{Name: "server"}, accounts, []string{"ssh"})
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 24, dialog: dialog}
	geometry := tui.dialogGeometry()
	render := func() []string {
		lines := make([]string, tui.height)
		for index := range lines {
			lines[index] = strings.Repeat(" ", tui.width)
		}
		tui.renderDialog(lines)
		return lines
	}
	lines := render()
	list := strings.Join(lines[geometry.y+7:geometry.y+7+geometry.rows], "\n")
	if !strings.Contains(list, "█") || !strings.Contains(list, "░") {
		t.Fatalf("long account list does not show a scrollbar: %q", list)
	}
	dialog.accountIndex = len(accounts) - 1
	lines = render()
	if !strings.Contains(lines[geometry.y+7+geometry.rows-1], "█") {
		t.Fatalf("account scrollbar did not move to the bottom: %q", lines[geometry.y+7+geometry.rows-1])
	}
	dialog.accountIndex = 0
	dialog.accountScroll = 0
	scrollbarX := geometry.x + geometry.width - 2
	listY := geometry.y + 7
	tui.Update(tea.MouseMsg{
		X: scrollbarX, Y: listY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !dialog.scrollbarDragging {
		t.Fatal("account scrollbar drag did not start")
	}
	tui.Update(tea.MouseMsg{
		X: scrollbarX, Y: listY + geometry.rows - 1,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion,
	})
	wantStart := len(accounts) - geometry.rows
	if dialog.accountScroll != wantStart || dialog.accountIndex != wantStart {
		t.Fatalf("account scrollbar did not drag to the bottom: scroll=%d index=%d", dialog.accountScroll, dialog.accountIndex)
	}
	tui.Update(tea.MouseMsg{X: scrollbarX, Y: listY, Action: tea.MouseActionRelease})
	if dialog.scrollbarDragging {
		t.Fatal("account scrollbar drag did not stop")
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
	if !strings.Contains(lines[geometry.y+6], "│  Account") ||
		strings.Contains(lines[geometry.y+6], "/ Search") {
		t.Fatalf("default account label is unexpected: %q", lines[geometry.y+6])
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

func TestAssetTUIDetailDialogSourcesAndPosition(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en"}
	tui := assetTUI{
		handler: handler, selector: &UserSelectHandler{}, width: 100, height: 30,
		assets: []model.PermAsset{{
			ID: "asset-1", Name: "Server", Address: "192.0.2.1", IsActive: true,
			Comment:  "This comment is long enough to wrap and its ending must remain visible.",
			Category: "host", Type: "linux", OrgName: "Default",
		}},
	}
	tui.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if tui.detailDialog == nil || tui.detailDialog.title != "Server" {
		t.Fatalf("space did not open asset details: %#v", tui.detailDialog)
	}
	geometry := tui.detailDialogGeometry()
	if geometry.width != 56 || geometry.x+geometry.width != tui.width-1 ||
		geometry.y+geometry.height != tui.height-1 {
		t.Fatalf("detail dialog is not at the bottom right: %#v", geometry)
	}
	displayRows, _ := tui.detailDialogRows(geometry.width)
	if len(displayRows) < 3 || !displayRows[1].separator {
		t.Fatalf("detail fields have no separator between them: %#v", displayRows)
	}
	detailLines := strings.Split(tui.View(), "\n")
	if separatorLine := detailLines[geometry.y+4]; !strings.Contains(separatorLine, " │ ") ||
		strings.ContainsAny(separatorLine, "├┼┤") {
		t.Fatalf("detail field spacing does not preserve only the vertical separator: %q", separatorLine)
	}
	if !strings.Contains(detailLines[geometry.y+geometry.height-3], "├") {
		t.Fatalf("detail shortcuts have no separator: %q", detailLines[geometry.y+geometry.height-3])
	}
	if view := tui.View(); !strings.Contains(view, "Address") || !strings.Contains(view, "192.0.2.1") {
		t.Fatalf("asset details are incomplete: %q", view)
	}
	if len(tui.detailDialog.rows) != 10 {
		t.Fatalf("asset details contain unexpected fields: %#v", tui.detailDialog.rows)
	}
	keys := make([]string, 0, len(tui.detailDialog.rows))
	for _, row := range tui.detailDialog.rows {
		keys = append(keys, row.key)
	}
	if got := strings.Join(keys, ","); got != "ID,Name,Address,Comment,Platform,Category,Type,Organization,Active,Protocol" {
		t.Fatalf("asset details do not use field labels: %q", got)
	}
	if got := tui.detailDialog.rows[len(tui.detailDialog.rows)-2].value; got != "Yes" {
		t.Fatalf("active asset does not use a localized yes value: %q", got)
	}
	tui.Update(assetTUIAssetDetailMsg{asset: tui.assets[0], protocols: []string{"ssh", "telnet"}})
	if got := tui.detailDialog.rows[len(tui.detailDialog.rows)-1].value; got != "ssh · telnet" {
		t.Fatalf("asset protocols are missing: %q", got)
	}
	inactiveTUI := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}}
	if got := inactiveTUI.assetDetailDialog(model.PermAsset{}, nil).rows[8].value; got != "No" {
		t.Fatalf("inactive asset does not use a localized no value: %q", got)
	}
	if view := tui.View(); !strings.Contains(view, "This comment is long enough") ||
		!strings.Contains(view, "remain visible.") {
		t.Fatalf("long asset details were truncated: %q", view)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tui.detailDialog != nil {
		t.Fatal("escape did not close asset details")
	}

	tui.dialog = newAssetTUIDialog(model.PermAsset{Name: "Server"}, []model.PermAccount{{
		Name: "Admin", Username: "root", SecretType: "password", HasSecret: true, Secret: "hidden",
		Actions: model.Actions{{Label: "Connect", Value: "connect"}, {Value: "upload"}},
	}}, []string{"ssh"})
	tui.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if tui.detailDialog == nil || tui.detailDialog.title != "root" {
		t.Fatalf("space did not open account details: %#v", tui.detailDialog)
	}
	if len(tui.detailDialog.rows) != 3 {
		t.Fatalf("account details contain unexpected fields: %#v", tui.detailDialog.rows)
	}
	if tui.detailDialog.rows[0].key != "Name" || tui.detailDialog.rows[1].key != "Username" ||
		tui.detailDialog.rows[2].key != "Actions" || tui.detailDialog.rows[2].value != "Connect · upload" {
		t.Fatalf("account details do not use field labels: %#v", tui.detailDialog.rows)
	}
	for _, row := range tui.detailDialog.rows {
		if strings.Contains(row.value, "hidden") {
			t.Fatalf("account details exposed a secret: %#v", row)
		}
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyEsc})

	cache := &assetTUITreeCache{
		kind: assetTUIAuthorizationTree, initialized: true, roots: []string{"root"},
		nodes: map[string]*assetTUITreeNode{
			"root": {identity: "root", id: "node-1", key: "1", name: "Root", count: 3},
		},
	}
	tui.trees = map[assetTUITreeKind]*assetTUITreeCache{assetTUIAuthorizationTree: cache}
	tui.treeDialog = &assetTUITreeDialog{kind: assetTUIAuthorizationTree}
	tui.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if tui.detailDialog == nil || tui.detailDialog.title != "Root" {
		t.Fatalf("space did not open node details: %#v", tui.detailDialog)
	}
	values := make(map[string]string, len(tui.detailDialog.rows))
	for _, row := range tui.detailDialog.rows {
		values[row.key] = row.value
	}
	if len(tui.detailDialog.rows) != 5 || values["ID"] != "node-1" || values["Key"] != "1" ||
		values["Path"] != "/Root" || values["Asset count"] != "3" {
		t.Fatalf("node details are incomplete: %#v", values)
	}
}

func TestUnavailableAssetMessageIncludesReason(t *testing.T) {
	selector := UserSelectHandler{h: &InteractiveHandler{i18nLang: "en"}}
	message := selector.unavailableAssetMessage(model.PermAsset{IsActive: false})
	if message != "Cannot connect: asset disabled." {
		t.Fatalf("unavailable asset reason is missing: %q", message)
	}
}
