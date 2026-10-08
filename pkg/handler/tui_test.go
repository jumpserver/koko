package handler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/mattn/go-runewidth"
	terminal "golang.org/x/term"

	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
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
	} else if !strings.Contains(line, " · Search:host") {
		t.Fatalf("search line does not contain the localized search prompt: %q", line)
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
	if !strings.HasPrefix(rendered, tuiResetViewport) {
		t.Fatalf("asset view did not reset the terminal viewport before rendering: %q", rendered)
	}
	if !strings.HasSuffix(rendered, "\x1b[1;20H"+tuiCursorBlinkRestore+tuiShowCursor) {
		t.Fatalf("terminal cursor is not positioned after the search input: %q", rendered)
	}
}

func TestAssetTUICursorWriterResetsViewportOnAlternateScreen(t *testing.T) {
	var output bytes.Buffer
	writer := assetTUICursorWriter{output: &output}
	if _, err := writer.Write([]byte(tuiEnterAltScreen)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), tuiEnterAltScreen+tuiResetViewport) {
		t.Fatalf("alternate screen did not reset the viewport: %q", output.String())
	}

	output.Reset()
	if _, err := writer.Write([]byte(tuiExitAltScreen)); err != nil {
		t.Fatal(err)
	}
	if output.String() != tuiResetViewport+tuiExitAltScreen {
		t.Fatalf("alternate screen exit did not reset the viewport: %q", output.String())
	}
}

func TestAssetTUISearchLineDefaultLabel(t *testing.T) {
	model := assetTUI{handler: &InteractiveHandler{i18nLang: "zh-CN"}, width: 80}
	label := "我的资产"
	if line := strings.TrimSpace(model.searchLine()); line != label {
		t.Fatalf("unexpected default search label: %q", line)
	}
	model.searching = true
	model.searchInput = []rune("主机")
	if line := strings.TrimSpace(model.searchLine()); line != label+" · 搜索:主机" {
		t.Fatalf("unexpected localized search prompt: %q", line)
	}
	model.searching = false
	model.query = "主机"
	if line := strings.TrimSpace(model.searchLine()); line != label+" · 搜索:主机" {
		t.Fatalf("submitted search changed the localized search prompt: %q", line)
	}
	if line, _ := tuiSearchField("账号", "搜索", []rune("root"), 40); strings.TrimSpace(line) != "账号 · 搜索:root" {
		t.Fatalf("unexpected account search prompt: %q", line)
	}
}

func TestTerminalWindowSupportsTUI(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          bool
	}{
		{width: 80, height: 24, want: true},
		{width: 79, height: 24, want: false},
		{width: 80, height: 23, want: false},
		{width: 0, height: 0, want: true},
	} {
		if got := terminalWindowSupportsTUI(test.width, test.height); got != test.want {
			t.Fatalf("terminalWindowSupportsTUI(%d, %d) = %v, want %v",
				test.width, test.height, got, test.want)
		}
	}
}

func TestTerminalTextModePriority(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		mode          terminalInterfaceMode
		want          bool
	}{
		{name: "large TUI", width: 80, height: 24, mode: terminalInterfaceModeTUI},
		{name: "large text", width: 80, height: 24, mode: terminalInterfaceModeText, want: true},
		{name: "narrow TUI", width: 79, height: 24, mode: terminalInterfaceModeTUI, want: true},
		{name: "short TUI", width: 80, height: 23, mode: terminalInterfaceModeTUI, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := terminalShouldUseTextMode(test.width, test.height, test.mode); got != test.want {
				t.Fatalf("terminalShouldUseTextMode() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTerminalPreferenceFile(t *testing.T) {
	userID := t.Name()
	store := newTerminalPreferenceStore(t.TempDir())
	asset := model.PermAsset{ID: "asset-1", OrgID: "org-1"}

	first := &InteractiveHandler{
		user: &model.User{ID: userID}, i18nLang: "en", preferenceStore: store,
	}
	first.loadTerminalPreference()
	if first.interfaceMode != terminalInterfaceModeTUI {
		t.Fatalf("unexpected default interface mode: %q", first.interfaceMode)
	}
	first.mouseMode = terminalMouseModeClient
	first.saveTerminalPreference(terminalInterfaceModeText)
	first.saveTerminalLanguage("zh")
	first.saveLastConnectionPreference(asset, model.PermAccount{
		Alias: "account-1", Name: "Root", Username: "root", Secret: "must-not-be-cached",
	}, "ssh")

	next := &InteractiveHandler{
		user: &model.User{ID: userID}, i18nLang: "en", preferenceStore: store,
	}
	next.loadTerminalPreference()
	if next.interfaceMode != terminalInterfaceModeText {
		t.Fatalf("cached interface mode was not restored: %q", next.interfaceMode)
	}
	if next.mouseMode != terminalMouseModeKoko {
		t.Fatalf("mouse mode must not persist across logins: %q", next.mouseMode)
	}
	if next.i18nLang != "zh" {
		t.Fatalf("cached language was not restored: %q", next.i18nLang)
	}
	recent := next.loadRecentConnectionPreferences(asset)
	if len(recent) != 1 || recent[0].AccountAlias != "account-1" ||
		recent[0].AccountName != "Root" || recent[0].AccountUsername != "root" ||
		recent[0].Protocol != "ssh" {
		t.Fatalf("recent connection preference was not restored: %#v", recent)
	}
	data, err := os.ReadFile(store.userFile(userID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-not-be-cached") {
		t.Fatal("account secret must not be cached")
	}
	if info, err := os.Stat(store.userFile(userID)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected preference file permissions: info=%v err=%v", info, err)
	}
}

func TestTerminalPreferenceKeepsFiveRecentConnectionsPerAsset(t *testing.T) {
	store := newTerminalPreferenceStore(t.TempDir())
	handler := &InteractiveHandler{
		user: &model.User{ID: t.Name()}, preferenceStore: store,
	}
	asset := model.PermAsset{ID: "asset-1", OrgID: "org-1"}
	otherAsset := model.PermAsset{ID: "asset-2", OrgID: "org-1"}
	for index := 1; index <= 6; index++ {
		handler.saveLastConnectionPreference(asset, model.PermAccount{
			Alias: fmt.Sprintf("account-%d", index),
		}, fmt.Sprintf("protocol-%d", index))
	}
	handler.saveLastConnectionPreference(otherAsset, model.PermAccount{Alias: "other"}, "ssh")

	recent := handler.loadRecentConnectionPreferences(asset)
	if len(recent) != terminalPreferenceRecentLimit {
		t.Fatalf("recent connection count = %d, want %d", len(recent), terminalPreferenceRecentLimit)
	}
	if recent[0].AccountAlias != "account-6" || recent[4].AccountAlias != "account-2" {
		t.Fatalf("unexpected recent connection order: %#v", recent)
	}
	otherRecent := handler.loadRecentConnectionPreferences(otherAsset)
	if len(otherRecent) != 1 || otherRecent[0].AccountAlias != "other" {
		t.Fatalf("other asset history was not isolated: %#v", otherRecent)
	}
}

func TestTerminalPreferenceReadFailureUsesDefaults(t *testing.T) {
	store := newTerminalPreferenceStore(t.TempDir())
	userID := t.Name()
	if err := os.WriteFile(store.userFile(userID), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := &InteractiveHandler{
		user: &model.User{ID: userID}, i18nLang: "ja", preferenceStore: store,
	}
	handler.loadTerminalPreference()
	if handler.interfaceMode != terminalInterfaceModeTUI || handler.i18nLang != "ja" {
		t.Fatalf("read failure did not preserve defaults: mode=%q language=%q",
			handler.interfaceMode, handler.i18nLang)
	}
}

func TestAssetTUITogglesMouseMode(t *testing.T) {
	handler := &InteractiveHandler{i18nLang: "en", mouseMode: terminalMouseModeKoko}
	tui := assetTUI{handler: handler}
	if _, cmd := tui.toggleMouseMode(); cmd == nil || handler.mouseMode != terminalMouseModeClient {
		t.Fatalf("mouse mode did not switch to local selection: mode=%q cmd=%v", handler.mouseMode, cmd)
	}
	if shortcut := tui.mouseModeShortcut(); shortcut != "v:Interface control (mouse)" {
		t.Fatalf("unexpected local selection shortcut: %q", shortcut)
	}
	if help := tui.mouseModeHelpDescription(); help != "Switch to interface control; Koko handles mouse clicks, scrolling, and UI actions" {
		t.Fatalf("unexpected local selection help: %q", help)
	}
	if _, cmd := tui.toggleMouseMode(); cmd == nil || handler.mouseMode != terminalMouseModeKoko {
		t.Fatalf("mouse mode did not switch back to Koko: mode=%q cmd=%v", handler.mouseMode, cmd)
	}
	if shortcut := tui.mouseModeShortcut(); shortcut != "v:Text selection (mouse)" {
		t.Fatalf("unexpected Koko mouse shortcut: %q", shortcut)
	}
}

func TestAssetTUIQuitConfirmation(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 80, height: 24, multiSessionCount: 4, unfinishedSessions: 3,
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd != nil || !tui.quitDialog {
		t.Fatalf("q exited without confirmation: dialog=%v cmd=%v", tui.quitDialog, cmd)
	}
	view := tui.View()
	for _, text := range []string{
		"Quit Koko", "Quit this SSH session?",
		"3 multi-sessions are still active; quitting will end them.",
		"enter:Confirm · esc:Cancel",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("quit confirmation is missing %q: %q", text, view)
		}
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil || tui.quitDialog {
		t.Fatalf("esc did not cancel quitting: dialog=%v cmd=%v", tui.quitDialog, cmd)
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil || !tui.quitDialog {
		t.Fatalf("ctrl+c exited without confirmation: dialog=%v cmd=%v", tui.quitDialog, cmd)
	}
	_, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not confirm quitting")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("enter did not return a quit command")
	}
}

func TestAssetTUITableRemovesInvisibleFormatCharacters(t *testing.T) {
	tui := assetTUI{
		handler:  &InteractiveHandler{i18nLang: "en"},
		selector: &UserSelectHandler{},
		width:    140,
		total:    1,
	}
	asset := model.PermAsset{
		Name: "redis", Address: "redis\u2060", OrgName: "DEFAULT",
		Platform: model.BasePlatform{Name: "Redis6+"},
	}
	row := tui.renderColumns(tui.columns(), &asset, 0)
	if strings.ContainsRune(row, '\u2060') {
		t.Fatalf("asset row retained an invisible format character: %q", row)
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
		},
		width: 80,
	}
	line := model.searchLine()
	if !strings.HasSuffix(line, "Alice | JumpServer") {
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
	tui.searchInput = []rune("host")
	tui.updateSearch(tea.KeyMsg{Type: tea.KeyCtrlH})
	if string(tui.searchInput) != "hos" {
		t.Fatalf("ctrl+h did not delete the previous search character: %q", tui.searchInput)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if tui.helpDialog || string(tui.searchInput) != "hos?" {
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
	if tui.cursor != 0 {
		t.Fatalf("up moved beyond the first asset: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tui.cursor != 1 {
		t.Fatalf("down did not move to the last asset: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if tui.cursor != len(tui.assets)-1 {
		t.Fatalf("j moved beyond the last asset: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if tui.cursor != 0 {
		t.Fatalf("k did not move back to the first asset: %d", tui.cursor)
	}
	tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if tui.cursor != 0 {
		t.Fatalf("k moved beyond the first asset: %d", tui.cursor)
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
	store := newTerminalPreferenceStore(t.TempDir())
	handler := &InteractiveHandler{
		user: &model.User{ID: t.Name()}, i18nLang: "en", preferenceStore: store,
	}
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
	reloaded := &InteractiveHandler{
		user: &model.User{ID: t.Name()}, i18nLang: "en", preferenceStore: store,
	}
	reloaded.loadTerminalPreference()
	if reloaded.i18nLang != "zh" {
		t.Fatalf("selected language was not persisted: %q", reloaded.i18nLang)
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
	for _, shortcut := range []string{"/:Search", "enter:Connect", "c:Direct connect", "space:Details", "?:View help"} {
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
		"enter":      "Connect to the selected asset",
		"c":          "Connect to the selected asset in a native single session",
		"d":          "Clear the selected tree node",
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
	footerSeparator := geometry.y + geometry.height - 3
	if !strings.Contains(lines[footerSeparator], "├") || !strings.Contains(lines[footerSeparator], "┤") ||
		!strings.Contains(lines[footerSeparator+1], "esc:Cancel") {
		t.Fatalf("help cancel footer is not separated from its content: %q / %q",
			lines[footerSeparator], lines[footerSeparator+1])
	}
	protocolContent := strings.Join(lines[protocolStart+1:footerSeparator], "\n")
	if !strings.Contains(lines[protocolStart+1], "Protocols supported by the current terminal:") {
		t.Fatalf("supported protocol title is missing: %q", protocolContent)
	}
	if protocols := srvconn.SupportedProtocols(); len(protocols) > 0 &&
		strings.Contains(lines[protocolStart+1], protocols[0]) {
		t.Fatalf("supported protocols start on the title row: %q", lines[protocolStart+1])
	}
	protocolList := strings.Join(lines[protocolStart+2:footerSeparator], "\n")
	for _, protocol := range srvconn.SupportedProtocols() {
		if !strings.Contains(protocolList, protocol) {
			t.Fatalf("supported protocol %q is missing: %q", protocol, protocolList)
		}
	}
	for row := protocolStart + 1; row < footerSeparator; row++ {
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
	if !strings.Contains(scrolled, "?") || strings.Contains(scrolled, "esc") {
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
	if line := tui.searchLine(); !strings.Contains(line, " · Search:server") {
		t.Fatalf("scoped search input is not visible after the path: %q", line)
	}
	tui.searching = false
	tui.query = "server"
	if line := tui.searchLine(); !strings.Contains(line, " · Search:server") {
		t.Fatalf("submitted scoped search changed the prompt format: %q", line)
	}
	tui.multiSessionCount = 9
	tui.width = 500
	footer := tui.footerLine()
	for _, shortcut := range []string{"/:Search", "x:Clear search", "enter:Connect", "c:Direct connect", "w:Sessions(9/9)", "space:Details", "g:Asset tree", "d:Clear node", "v:Text selection (mouse)", "t:Text mode", "q:Quit", "?:View help"} {
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
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}); cmd == nil ||
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

func TestAssetTUIPaginatedListKeepsSeparatorAboveStatus(t *testing.T) {
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
		t.Fatalf("separator does not follow the paginated asset list: %q", lines[8])
	}
	if !strings.HasPrefix(lines[9], tuiStatusPrefix+"Loaded") ||
		!strings.HasSuffix(lines[9], "1-5/8") {
		t.Fatalf("status and page line is not fixed above the shortcut line: %q", lines[9])
	}
	if !strings.Contains(lines[10], "?:View help") {
		t.Fatalf("shortcut line does not follow the status and page line: %q", lines[10])
	}
	if len(lines) != tui.height {
		t.Fatalf("asset view has %d rows, want %d", len(lines), tui.height)
	}
}

func TestAssetTUIFullscreenHeightKeepsFooterAtBottom(t *testing.T) {
	tui := assetTUI{
		handler: &InteractiveHandler{i18nLang: "en"}, selector: &UserSelectHandler{},
		width: 80, height: 24,
	}
	_, _ = tui.Update(tea.WindowSizeMsg{Width: 160, Height: 240})
	if tui.height != 240 {
		t.Fatalf("fullscreen terminal height was reduced to %d", tui.height)
	}
	tui.total = 1000
	tui.assets = make([]model.PermAsset, tui.visibleAssetRows())
	tui.connectable = make([]bool, len(tui.assets))
	lines := strings.Split(tui.View(), "\n")
	if len(lines) != 240 {
		t.Fatalf("fullscreen asset view has %d rows, want 240", len(lines))
	}
	if strings.TrimSpace(lines[len(lines)-3]) != strings.Repeat("─", tui.width) {
		t.Fatalf("fullscreen pagination separator is not above the status row: %q", lines[len(lines)-3])
	}
	if !strings.HasSuffix(lines[len(lines)-2], fmt.Sprintf("1-%d/1000", len(tui.assets))) {
		t.Fatalf("fullscreen pagination is not on the penultimate row: %q", lines[len(lines)-2])
	}
	if !strings.Contains(lines[len(lines)-1], "?:View help") {
		t.Fatalf("fullscreen shortcuts are not on the final row: %q", lines[len(lines)-1])
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
	if _, cmd := tui.Update(click); cmd == nil || !tui.loadingChoices || !tui.pendingMultiWindow {
		t.Fatal("second click should load account and protocol choices for a multi-session connection")
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
	if tui.connection.account.Alias != "account-1" || tui.connection.protocol != "ssh" {
		t.Fatalf("unexpected connection selection: %#v", tui.connection)
	}
}

func TestAssetTUIMultiSessionSelectionAndWorkspaceShortcut(t *testing.T) {
	tui := assetTUI{
		handler:            &InteractiveHandler{i18nLang: "en"},
		pendingMultiWindow: true,
		multiSessionCount:  1,
	}
	if _, cmd := tui.Update(assetChoicesMsg{
		asset:     model.PermAsset{ID: "asset-1"},
		accounts:  []model.PermAccount{{Alias: "account-1"}},
		protocols: []string{"ssh"},
	}); cmd == nil || tui.connection == nil || !tui.connection.multiWindow {
		t.Fatalf("multi-session selection did not create a multi-window connection: %#v", tui.connection)
	}

	tui.connection = nil
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}}); cmd == nil || !tui.showMultiSessions {
		t.Fatal("w did not return to the existing multi-session workspace")
	}
}

func TestAssetTUIConnectionKeysChooseMode(t *testing.T) {
	tui := assetTUI{
		handler:     &InteractiveHandler{i18nLang: "en"},
		selector:    &UserSelectHandler{},
		assets:      []model.PermAsset{{ID: "asset-1"}},
		connectable: []bool{true},
	}
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || !tui.pendingMultiWindow {
		t.Fatal("enter did not select multi-session connection mode")
	}
	tui.loadingChoices = false
	if _, cmd := tui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}); cmd == nil || tui.pendingMultiWindow {
		t.Fatal("c did not select native single-session connection mode")
	}
}

func TestAssetTUIMultiSessionTabsFitWidth(t *testing.T) {
	if assetTUIMaxMultiSessions != 9 {
		t.Fatalf("multi-session limit is %d, want 9", assetTUIMaxMultiSessions)
	}
	sessions := []*assetTUIMultiSession{
		{title: "数据库资产"},
		{title: "server", done: true},
	}
	line := assetTUIMultiTabsLine(48, sessions, 1)
	plain := strings.ReplaceAll(strings.ReplaceAll(line, tuiSelectedStyle, ""), tuiStyleReset, "")
	if width := runewidth.StringWidth(plain); width != 48 {
		t.Fatalf("multi-session tabs have width %d, want 48: %q", width, line)
	}
	if !strings.HasPrefix(plain, "   1:数据库资产   | ▶ 2:server ×   ") {
		t.Fatalf("multi-session tabs do not show the active marker and session numbers: %q", line)
	}
	if !strings.HasPrefix(line, tuiSelectedStyle+"   1:") ||
		strings.Count(line, tuiSelectedStyle) != 1 ||
		strings.Count(line, tuiStyleReset) != 1 ||
		!strings.HasSuffix(line, tuiStyleReset) {
		t.Fatalf("the complete tab row does not keep one selected style: %q", line)
	}
	if height := assetTUIMultiContentHeight(24); height != 21 {
		t.Fatalf("multi-session content height is %d, want 21", height)
	}
	_, hits := assetTUIMultiTabsLayout(48, sessions, 1)
	if len(hits) != 2 || hits[0].start != 0 || hits[1].start != hits[0].end+1 {
		t.Fatalf("unexpected multi-session tab hit areas: %#v", hits)
	}
	if inactive := assetTUIMultiTabLabel("1:host", false, 12); inactive != "   1:host   " {
		t.Fatalf("inactive tab padding is incorrect: %q", inactive)
	}
	if active := assetTUIMultiTabLabel("1:host", true, 12); active != " ▶ 1:host   " {
		t.Fatalf("active tab marker is not centered in the left padding: %q", active)
	}
	longTitle := strings.Repeat("database-server-", 3)
	doneBody := assetTUIMultiTabBody(0, &assetTUIMultiSession{title: longTitle, done: true}, 24)
	if runewidth.StringWidth(doneBody) != 24 || !strings.Contains(doneBody, "…") ||
		!strings.HasSuffix(doneBody, " ×") {
		t.Fatalf("a truncated completed-session tab lost its status: %q", doneBody)
	}
	failedLine := assetTUIMultiTabsLine(18, []*assetTUIMultiSession{{
		title: longTitle, done: true, failed: true,
	}}, 0)
	failedPlain := strings.ReplaceAll(strings.ReplaceAll(failedLine, tuiSelectedStyle, ""), tuiStyleReset, "")
	if runewidth.StringWidth(failedPlain) != 18 ||
		!strings.HasSuffix(strings.TrimRight(failedPlain, " "), " !") {
		t.Fatalf("a narrow failed-session tab lost its status: %q", failedLine)
	}
	overflowSessions := []*assetTUIMultiSession{
		{title: "one"}, {title: "two"}, {title: "current"}, {title: "four"}, {title: "five"},
	}
	overflowLine, overflowHits := assetTUIMultiTabsLayout(22, overflowSessions, 2)
	overflowPlain := strings.ReplaceAll(strings.ReplaceAll(overflowLine, tuiSelectedStyle, ""), tuiStyleReset, "")
	if runewidth.StringWidth(overflowPlain) != 22 || !strings.HasPrefix(overflowPlain, "‹2| ▶ 3:current   |2›") ||
		strings.Contains(overflowPlain, "two") || len(overflowHits) != 3 ||
		!overflowHits[0].sessionList || overflowHits[1].index != 2 || !overflowHits[2].sessionList {
		t.Fatalf("overflowing tabs do not keep the active session with clickable counts: %q %#v",
			overflowLine, overflowHits)
	}
	overflowLine = assetTUIMultiTabsLine(22, overflowSessions, 4)
	overflowPlain = strings.ReplaceAll(strings.ReplaceAll(overflowLine, tuiSelectedStyle, ""), tuiStyleReset, "")
	if !strings.HasPrefix(overflowPlain, "‹4| ▶ 5:five   ") {
		t.Fatalf("tab window did not slide with the active session: %q", overflowLine)
	}
	event, consumed := assetTUIMultiMouse([]byte("\x1b[<0;5;23M"))
	if consumed != len("\x1b[<0;5;23M") || !event.press || event.button != 0 || event.x != 4 || event.y != 22 {
		t.Fatalf("unexpected parsed tab click: event=%#v consumed=%d", event, consumed)
	}
	legacyMouse := []byte{0x1b, '[', 'M', 32 + 64, 33 + 4, 33 + 21}
	event, consumed = assetTUIMultiMouse(legacyMouse)
	if consumed != len(legacyMouse) || !event.press || event.button != 64 || event.x != 4 || event.y != 21 {
		t.Fatalf("unexpected parsed X10 mouse wheel: event=%#v consumed=%d", event, consumed)
	}
	legacyMouse = []byte{0x1b, '[', 'M', 32 + 2, 33 + 7, 33 + 3}
	event, consumed = assetTUIMultiMouse(legacyMouse)
	if consumed != len(legacyMouse) || !event.press || event.button != 2 || event.x != 7 || event.y != 3 {
		t.Fatalf("unexpected parsed X10 right click: event=%#v consumed=%d", event, consumed)
	}
}

func TestAssetTUIMultiSessionLimitUsesTemporaryNotice(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()
	_, _ = screen.Write([]byte("prompt$ "))
	before := strings.Join(screen.Rows(), "\n")

	sessions := make([]*assetTUIMultiSession, assetTUIMaxMultiSessions)
	for index := range sessions {
		sessions[index] = &assetTUIMultiSession{title: fmt.Sprintf("session-%d", index+1)}
	}
	sessions[len(sessions)-1].screen = screen
	manager := &assetTUIMultiSessionManager{
		handler: &InteractiveHandler{i18nLang: "en"},
		active:  len(sessions) - 1, sessions: sessions,
	}
	leave, err := manager.handleCommand('d')
	if err != nil || leave {
		t.Fatalf("session limit returned as a command error: leave=%v err=%v", leave, err)
	}
	if !strings.Contains(manager.notice, "at most 9 session windows") {
		t.Fatalf("session limit notice is missing: %q", manager.notice)
	}
	if after := strings.Join(screen.Rows(), "\n"); after != before {
		t.Fatalf("session limit message polluted the active session: before=%q after=%q", before, after)
	}
	if assetTUIMultiNoticeDuration != 3*time.Second {
		t.Fatalf("session limit notice duration is %s, want 3s", assetTUIMultiNoticeDuration)
	}
	left, top, lines := assetTUIMultiNoticeLayout(80, 24, manager.notice)
	_, contentBottom := assetTUIMultiContentBounds(24)
	if len(lines) != 3 || left+runewidth.StringWidth(lines[0])-1 != 80 ||
		top+len(lines)-1 != contentBottom {
		t.Fatalf("notice is not aligned to the bottom right: left=%d top=%d lines=%#v",
			left, top, lines)
	}
}

func TestAssetTUIMultiSessionShortcutMode(t *testing.T) {
	translate := func(_, english string) string { return english }
	footerSession := &assetTUIMultiSession{connection: assetTUIConnection{
		asset: model.PermAsset{Name: "db.example.com"},
		account: model.PermAccount{
			Username: "root",
		},
		protocol: "ssh",
	}}
	normal := assetTUIMultiFooterLine(80, false, 1, footerSession, translate)
	if runewidth.StringWidth(normal) != 80 || !strings.HasPrefix(normal, "ctrl+b:Sessions(1/9)") ||
		!strings.HasSuffix(normal, "ssh://root@db.example.com") {
		t.Fatalf("unexpected focused-session footer: %q", normal)
	}
	if separator := assetTUIMultiMiddleLine(80, false, "tabs"); separator != "" {
		t.Fatalf("focused session still shows a separator above its footer: %q", separator)
	}
	if tabs := assetTUIMultiMiddleLine(80, true, "tabs"); tabs != "tabs" {
		t.Fatalf("session workspace did not keep the tab row: %q", tabs)
	}
	active := assetTUIMultiFooterLine(300, true, 1, footerSession, translate)
	wantActive := "enter:Enter session · tab:Next session · d:Duplicate session · r:Reconnect session · " +
		"z:Immersive mode · x:Close session · q:Close all sessions · a:Asset list · ?:View help"
	if strings.TrimSpace(active) != wantActive {
		t.Fatalf("active shortcut footer has an unexpected order: %q", active)
	}
	if assetTUIMultiSessionCursorVisible(true, 0) {
		t.Fatal("shortcut mode left the session cursor visible")
	}
	if !assetTUIMultiSessionCursorVisible(false, 0) {
		t.Fatal("entering the session did not restore the session cursor")
	}
	if assetTUIMultiSessionCursorVisible(false, 1) {
		t.Fatal("scrollback view left the session cursor visible")
	}
	if mode := assetTUIMultiMouseTracking(false, true); mode != assetTUIMouseDisable {
		t.Fatalf("focused session did not release terminal mouse control: %q", mode)
	}
	if mode := assetTUIMultiMouseTracking(true, true); mode != assetTUIMouseEnable {
		t.Fatalf("shortcut mode did not capture terminal mouse control: %q", mode)
	}
	if mode := assetTUIMultiMouseTracking(true, false); mode != assetTUIMouseDisable {
		t.Fatalf("local selection mode did not keep mouse control in the terminal: %q", mode)
	}

	manager := &assetTUIMultiSessionManager{commandMode: true}
	manager.height = 24
	manager.handleMouse(assetTUIMultiMouseEvent{button: 0, x: 5, y: 5, press: true})
	if manager.commandMode {
		t.Fatal("clicking the session content did not enter the current session")
	}
	manager.commandMode = true
	if leave, err := manager.handleCommand('\t'); err != nil || leave || !manager.commandMode {
		t.Fatalf("tab did not keep shortcut mode active: leave=%v err=%v command=%v",
			leave, err, manager.commandMode)
	}
	leave, err := manager.handleCommand('?')
	if err != nil || leave || !manager.commandMode || !manager.helpVisible {
		t.Fatalf("question mark did not open shortcut help: leave=%v err=%v command=%v help=%v",
			leave, err, manager.commandMode, manager.helpVisible)
	}
	manager.closeHelp()
	if !manager.commandMode || manager.helpVisible {
		t.Fatalf("closing help did not return to shortcut mode: command=%v help=%v",
			manager.commandMode, manager.helpVisible)
	}
	if leave, err = manager.handleCommand(0x1b); err != nil || leave || manager.commandMode {
		t.Fatalf("esc did not leave shortcut mode: leave=%v err=%v command=%v",
			leave, err, manager.commandMode)
	}
	manager.commandMode = true
	if leave, err = manager.handleCommand('i'); err != nil || leave || manager.commandMode {
		t.Fatalf("i did not enter the current session: leave=%v err=%v command=%v",
			leave, err, manager.commandMode)
	}
	manager.commandMode = true
	if leave, err = manager.handleCommand('\r'); err != nil || leave || manager.commandMode {
		t.Fatalf("enter did not enter the current session: leave=%v err=%v command=%v",
			leave, err, manager.commandMode)
	}
	ctx, cancelInput := context.WithCancel(context.Background())
	defer cancelInput()
	input := make(chan []byte, 1)
	inputSession := &assetTUIMultiSession{scrollOffset: 3}
	inputSession.conn = &assetTUIMultiUserConnection{ctx: ctx, cancel: cancelInput, input: input}
	inputManager := &assetTUIMultiSessionManager{
		commandMode: true,
		sessions:    []*assetTUIMultiSession{inputSession},
	}
	if leave, err = inputManager.handleCommand('\r'); err != nil || leave || inputManager.commandMode ||
		inputSession.scrollOffset != 0 {
		t.Fatalf("enter did not restore the live session view: leave=%v err=%v command=%v offset=%d",
			leave, err, inputManager.commandMode, inputSession.scrollOffset)
	}
	inputManager.forward([]byte("typed"))
	select {
	case data := <-input:
		if string(data) != "typed" {
			t.Fatalf("unexpected input after leaving shortcut mode: %q", data)
		}
	default:
		t.Fatal("input was not forwarded after leaving shortcut mode")
	}
	for sequence, want := range map[string]byte{
		"\x1b[A": 'u', "\x1b[B": 'd', "\x1b[C": 'r', "\x1b[D": 'l',
		"\x1bOA": 'u', "\x1bOB": 'd', "\x1bOC": 'r', "\x1bOD": 'l',
	} {
		if direction, consumed := assetTUIMultiArrow([]byte(sequence)); direction != want || consumed != 3 {
			t.Fatalf("arrow %q parsed as direction=%q consumed=%d", sequence, direction, consumed)
		}
	}
	for _, sequence := range []string{"\x1b[Z", "\x1b[1;2Z", "\x9bZ"} {
		if consumed := assetTUIMultiShiftTab([]byte(sequence)); consumed != len(sequence) {
			t.Fatalf("shift+tab %q consumed %d bytes, want %d", sequence, consumed, len(sequence))
		}
	}
	helpRows := assetTUIMultiHelpRows(translate)
	for _, key := range []string{"tab", "shift+tab", "s", "←, →, h, l", "1-9", "↑, ↓, j, k", "x", "d", "r", "z", "a", "q", "?", "enter, esc, i"} {
		found := false
		for _, row := range helpRows {
			found = found || row.key == key
		}
		if !found {
			t.Fatalf("multi-session help is missing %q", key)
		}
	}
	rows := helpDialogRows(helpRows)
	if len(rows) != 29 || !rows[1].separator || rows[len(rows)-1].key != "enter, esc, i" ||
		rows[len(rows)-1].description != "Enter the current session" {
		t.Fatalf("multi-session help does not use spaced help rows: %#v", rows)
	}
	for _, row := range helpRows {
		if row.key == "esc" {
			t.Fatalf("multi-session help cancel action is still mixed into the content rows: %#v", helpRows)
		}
	}
	for index, row := range helpRows {
		if row.key == "q" && (index == 0 || helpRows[index-1].key != "x") {
			t.Fatalf("close-session shortcut is not immediately before close-all: %#v", helpRows)
		}
	}
	geometry, ok := assetTUIMultiSessionListLayout(40, 12, 9)
	if !ok || geometry.left != 1 || geometry.top != 1 || geometry.rows != 2 {
		t.Fatalf("unexpected compact session-list geometry: %#v ok=%v", geometry, ok)
	}
	mouseManager := &assetTUIMultiSessionManager{
		width: 80, height: 24, commandMode: true, sessionListVisible: true,
		sessions: []*assetTUIMultiSession{{title: "one"}, {title: "two"}, {title: "three"}},
	}
	mouseGeometry, ok := assetTUIMultiSessionListLayout(
		mouseManager.width, mouseManager.height, len(mouseManager.sessions),
	)
	if !ok {
		t.Fatal("session-list mouse test has no popup geometry")
	}
	mouseManager.handleSessionListMouse(assetTUIMultiMouseEvent{
		button: 0, x: mouseGeometry.left + 2, y: mouseGeometry.top + 3, press: true,
	})
	if !mouseManager.sessionListVisible || mouseManager.active != 1 {
		t.Fatalf("clicking a session did not keep the popup open and select it: list=%v active=%d",
			mouseManager.sessionListVisible, mouseManager.active)
	}
	mouseManager.handleSessionListMouse(assetTUIMultiMouseEvent{
		button: 0, x: mouseGeometry.left - 2, y: mouseGeometry.top + 3, press: true,
	})
	if mouseManager.sessionListVisible || mouseManager.active != 1 {
		t.Fatalf("clicking outside did not close only the session list: list=%v active=%d",
			mouseManager.sessionListVisible, mouseManager.active)
	}
	manager = &assetTUIMultiSessionManager{
		commandMode: true,
		active:      1,
		sessions: []*assetTUIMultiSession{
			{title: "one"}, {title: "two"}, {title: "three"},
		},
	}
	if leave, err = manager.handleCommand('s'); err != nil || leave || !manager.sessionListVisible ||
		manager.sessionListIndex != 1 {
		t.Fatalf("s did not open the session list at the active session: leave=%v err=%v manager=%#v",
			leave, err, manager)
	}
	manager.moveSessionList(1)
	if manager.active != 2 || !manager.sessionListVisible || !manager.commandMode {
		t.Fatalf("moving the list selection did not immediately activate the session: active=%d list=%v command=%v",
			manager.active, manager.sessionListVisible, manager.commandMode)
	}
	manager.moveSessionList(1)
	if manager.active != 2 || manager.sessionListIndex != 2 {
		t.Fatalf("session list moved beyond the last session: active=%d selected=%d",
			manager.active, manager.sessionListIndex)
	}
	manager.selectSessionList(0)
	manager.moveSessionList(-1)
	if manager.active != 0 || manager.sessionListIndex != 0 || !manager.sessionListVisible {
		t.Fatalf("session list moved beyond the first session: active=%d selected=%d list=%v",
			manager.active, manager.sessionListIndex, manager.sessionListVisible)
	}
	manager.closeSessionList(false)
	if manager.sessionListVisible || !manager.commandMode {
		t.Fatalf("esc did not close only the session list: list=%v command=%v",
			manager.sessionListVisible, manager.commandMode)
	}
	manager.switchSession(-1)
	if manager.active != 0 {
		t.Fatalf("session navigation moved left beyond the first session: %d", manager.active)
	}
	manager.switchSession(2)
	manager.switchSession(1)
	if manager.active != 2 {
		t.Fatalf("session navigation moved right beyond the last session: %d", manager.active)
	}
	manager.active = 0
	manager.openSessionList()
	manager.moveSessionList(1)
	manager.sessions[1].scrollOffset = 2
	manager.closeSessionList(true)
	if manager.sessionListVisible || manager.commandMode || manager.active != 1 ||
		manager.sessions[1].scrollOffset != 0 {
		t.Fatalf("enter did not enter the selected live session: active=%d list=%v command=%v offset=%d",
			manager.active, manager.sessionListVisible, manager.commandMode,
			manager.sessions[1].scrollOffset)
	}
	labelSession := &assetTUIMultiSession{connection: assetTUIConnection{
		asset:   model.PermAsset{Name: "db.example.com"},
		account: model.PermAccount{Username: "root"}, protocol: "ssh",
	}}
	if label := assetTUIMultiSessionListLabel(1, labelSession); label != "2. ssh://root@db.example.com" {
		t.Fatalf("unexpected session-list label: %q", label)
	}
	listShortcuts := strings.TrimSpace(assetTUIMultiSessionListShortcuts(80, translate))
	if listShortcuts != "↑, ↓:Select · enter:Enter session · esc:Close" {
		t.Fatalf("unexpected session-list shortcuts: %q", listShortcuts)
	}

	manager = &assetTUIMultiSessionManager{commandMode: true}
	contexts := make([]context.Context, 0, 2)
	for index := 0; index < 2; index++ {
		ctx, cancel := context.WithCancel(context.Background())
		screen, screenErr := newAssetTUIMultiScreen(80, 20)
		if screenErr != nil {
			t.Fatal(screenErr)
		}
		session := &assetTUIMultiSession{screen: screen}
		session.conn = &assetTUIMultiUserConnection{ctx: ctx, cancel: cancel}
		manager.sessions = append(manager.sessions, session)
		contexts = append(contexts, ctx)
	}
	manager.sessions[1].done = true
	if manager.UnfinishedCount() != 1 {
		t.Fatalf("unfinished session count is %d, want 1", manager.UnfinishedCount())
	}
	if leave, err = manager.handleCommand('x'); err != nil || leave ||
		manager.confirmAction != assetTUIMultiConfirmCloseCurrent || manager.Count() != 2 {
		t.Fatalf("x did not request confirmation: leave=%v err=%v action=%d count=%d",
			leave, err, manager.confirmAction, manager.Count())
	}
	if leave = manager.handleConfirmationKey(0x1b); leave ||
		manager.confirmAction != assetTUIMultiConfirmNone || manager.Count() != 2 {
		t.Fatalf("esc did not cancel close-current confirmation: leave=%v action=%d count=%d",
			leave, manager.confirmAction, manager.Count())
	}
	if leave, err = manager.handleCommand('x'); err != nil || leave {
		t.Fatalf("x did not reopen confirmation: leave=%v err=%v", leave, err)
	}
	if leave = manager.handleConfirmationKey('\r'); leave || manager.Count() != 1 {
		t.Fatalf("confirming x did not close only the current session: leave=%v count=%d",
			leave, manager.Count())
	}
	if leave, err = manager.handleCommand('q'); err != nil || leave ||
		manager.confirmAction != assetTUIMultiConfirmCloseAll || manager.Count() != 1 {
		t.Fatalf("q did not request confirmation: leave=%v err=%v action=%d count=%d",
			leave, err, manager.confirmAction, manager.Count())
	}
	if leave = manager.handleConfirmationKey('\r'); !leave || manager.Count() != 0 {
		t.Fatalf("confirming q did not close all sessions and leave: leave=%v count=%d",
			leave, manager.Count())
	}
	for _, ctx := range contexts {
		if ctx.Err() != context.Canceled {
			t.Fatalf("q did not cancel a closed session: %v", ctx.Err())
		}
	}
	left, top, popupWidth, ok := assetTUIMultiConfirmationLayout(
		80, 24, "Close all sessions", "Close all sessions and return to the asset list?",
		"enter:Confirm · esc:Cancel",
	)
	if !ok || left != 13 || top != 8 || popupWidth != 56 {
		t.Fatalf("unexpected confirmation geometry: left=%d top=%d width=%d ok=%v",
			left, top, popupWidth, ok)
	}

	activeSession := &assetTUIMultiSession{}
	backgroundSession := &assetTUIMultiSession{}
	finishedManager := &assetTUIMultiSessionManager{
		sessions:   []*assetTUIMultiSession{activeSession, backgroundSession},
		viewActive: true,
	}
	if !finishedManager.markFinished(activeSession, false) ||
		finishedManager.confirmAction != assetTUIMultiConfirmCloseEnded ||
		!finishedManager.commandMode || !activeSession.done || activeSession.failed {
		t.Fatalf("a cleanly ended active session did not request closing its tab: action=%d command=%v done=%v failed=%v",
			finishedManager.confirmAction, finishedManager.commandMode,
			activeSession.done, activeSession.failed)
	}
	backgroundManager := &assetTUIMultiSessionManager{
		sessions:   []*assetTUIMultiSession{activeSession, backgroundSession},
		viewActive: true,
	}
	if backgroundManager.markFinished(backgroundSession, false) ||
		backgroundManager.confirmAction != assetTUIMultiConfirmNone {
		t.Fatalf("an ended background session interrupted the active session: action=%d",
			backgroundManager.confirmAction)
	}
	failedSession := &assetTUIMultiSession{}
	failedManager := &assetTUIMultiSessionManager{
		sessions:   []*assetTUIMultiSession{failedSession},
		viewActive: true,
	}
	if failedManager.markFinished(failedSession, true) ||
		failedManager.confirmAction != assetTUIMultiConfirmNone || !failedSession.failed {
		t.Fatalf("a failed session incorrectly requested closing its tab: action=%d failed=%v",
			failedManager.confirmAction, failedSession.failed)
	}
	title, message, shortcuts := assetTUIMultiConfirmationText(
		assetTUIMultiConfirmCloseEnded, translate,
	)
	if title != "Session ended" ||
		message != "The current session has ended. Close this tab?" ||
		shortcuts != "enter:Close · esc:Keep" {
		t.Fatalf("unexpected ended-session confirmation: %q %q %q", title, message, shortcuts)
	}
}

func TestAssetTUIMultiSessionViewport(t *testing.T) {
	rows := []string{"one", "two", "three", "four", "five"}
	visible, offset := assetTUIMultiViewport(rows, 3, 0)
	if offset != 0 || strings.Join(visible, ",") != "three,four,five" {
		t.Fatalf("latest session viewport is incorrect: offset=%d rows=%#v", offset, visible)
	}
	visible, offset = assetTUIMultiViewport(rows, 3, 1)
	if offset != 1 || strings.Join(visible, ",") != "two,three,four" {
		t.Fatalf("scrolled session viewport is incorrect: offset=%d rows=%#v", offset, visible)
	}
	if region := assetTUIMultiScrollRegion(24, true); region != "\x1b[1;21r" {
		t.Fatalf("unexpected session-window scroll region: %q", region)
	}
	if region := assetTUIMultiScrollRegion(24, false); region != "\x1b[1;22r" {
		t.Fatalf("unexpected focused-session scroll region: %q", region)
	}
	if position := assetTUIMultiCursorPosition(80, 20, 11, 7); position !=
		"\x1b[8;12H"+tuiCursorBlinkRestore+tuiShowCursor {
		t.Fatalf("unexpected explicit session cursor position: %q", position)
	}
	if position := assetTUIMultiCursorRestore(80, 20, 11, 7, false); position !=
		"\x1b[8;12H"+tuiHideCursor {
		t.Fatalf("hidden session cursor lost its explicit position: %q", position)
	}
	older := assetTUIMultiScrollViewport(rows, 12, 6, 0, 1, false, true)
	if strings.Contains(older, utils.CharClear) || strings.Contains(older, tuiExitAltScreen) ||
		!strings.Contains(older, "\x1b[1T") || !strings.Contains(older, "two") {
		t.Fatalf("scrolling to older output repainted the workspace: %q", older)
	}
	newer := assetTUIMultiScrollViewport(rows, 12, 6, 1, 0, false, true)
	if strings.Contains(newer, utils.CharClear) || strings.Contains(newer, tuiExitAltScreen) ||
		!strings.Contains(newer, "\x1b[1S") || !strings.Contains(newer, "five") {
		t.Fatalf("scrolling to newer output repainted the workspace: %q", newer)
	}
	if height := assetTUIMultiSessionHeight(24, true, false); height != 24 {
		t.Fatalf("immersive session height is %d, want 24", height)
	}
	if height := assetTUIMultiSessionHeight(24, false, false); height != 22 {
		t.Fatalf("focused session height is %d, want 22", height)
	}
	if height := assetTUIMultiSessionHeight(24, false, true); height != 21 {
		t.Fatalf("session-window height is %d, want 21", height)
	}
	if !assetTUIMultiSessionContains(24, 0, true) || assetTUIMultiSessionContains(24, 24, true) ||
		!assetTUIMultiSessionContains(24, 0, false) || !assetTUIMultiSessionContains(24, 20, false) ||
		assetTUIMultiSessionContains(24, 21, false) {
		t.Fatal("immersive and standard session bounds overlap the chrome incorrectly")
	}
	if top := assetTUIMultiHintTop(24); top != 22 {
		t.Fatalf("immersive hint starts on row %d, want 22", top)
	}
}

func TestAssetTUIMultiSessionHistorySurvivesClear(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()
	if _, err = screen.Write([]byte("first\r\nsecond\r\nthird")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &assetTUIMultiSession{screen: screen}
	session.conn = &assetTUIMultiUserConnection{
		ctx: ctx, cancel: cancel, input: make(chan []byte, 1),
	}
	manager := &assetTUIMultiSessionManager{sessions: []*assetTUIMultiSession{session}}
	clearGeneration := screen.ClearGeneration()
	manager.forward([]byte{0x0c})
	manager.appendOutput(session, []byte("\r\nbefore-clear\r\n\x1b[H\x1b["))
	manager.appendOutput(session, []byte("2Jprompt\r\ncommand\r\nresult\r\nprompt"))
	rows := screen.Rows()
	output := strings.Join(rows, "\n")
	for _, value := range []string{"first", "second", "third", "before-clear", "command", "result"} {
		if !strings.Contains(output, value) {
			t.Fatalf("session history lost %q after clear: %q", value, output)
		}
	}
	if screen.ClearGeneration() != clearGeneration+1 {
		t.Fatal("ctrl+l clear-screen output did not preserve one history generation")
	}
	if maxOffset := max(0, len(rows)-2); maxOffset == 0 {
		t.Fatalf("preserved session history is not scrollable: %#v", rows)
	}
	viewRows := screen.ViewRows(8)
	renderRows := screen.RenderRows(8)
	for index, row := range renderRows {
		if strings.ContainsRune(row, '\r') {
			t.Fatalf("render row %d retains a carriage return that can erase history: %q", index, row)
		}
	}
	visible, offset := assetTUIMultiViewport(viewRows, 8, 0)
	wantVisible := strings.Join([]string{"prompt", "command", "result", "prompt", "", "", "", ""}, "\n")
	if offset != 0 || strings.Join(visible, "\n") != wantVisible {
		t.Fatalf("clear history leaked into the live session view: %#v", visible)
	}
	history, _ := assetTUIMultiViewport(viewRows, 8, len(viewRows))
	if !strings.Contains(strings.Join(history, "\n"), "first") {
		t.Fatalf("clear history is unavailable while scrolling: %#v", history)
	}
	renderHistory, _ := assetTUIMultiViewport(renderRows, 8, len(renderRows))
	if !strings.Contains(strings.Join(renderHistory, "\n"), "first") {
		t.Fatalf("styled clear history is unavailable while scrolling: %#v", renderHistory)
	}
	_, cursorY, _ := screen.Cursor()
	if cursorY != 3 {
		t.Fatalf("restored session cursor is on row %d, want row 3", cursorY)
	}
}

func TestAssetTUIMultiSessionHistorySurvivesRepeatedClear(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(40, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()

	_, _ = screen.Write([]byte("\x1b[32mprompt$\x1b[0m echo first\r\nfirst\r\n"))
	for _, command := range []string{"second", "third"} {
		screen.PreserveNextClear()
		_, _ = screen.Write([]byte("\x1b[H\x1b[2J\x1b[32mprompt$\x1b[0m echo " +
			command + "\r\n" + command + "\r\n\x1b[32mprompt$\x1b[0m "))
	}

	rows := screen.RenderRows(5)
	output := strings.Join(rows, "\n")
	for _, value := range []string{"echo first", "first", "echo second", "second", "echo third", "third"} {
		if !strings.Contains(output, value) {
			t.Fatalf("repeated clear lost session history %q: %q", value, output)
		}
	}
	for index, row := range rows {
		if strings.ContainsRune(row, '\r') {
			t.Fatalf("render row %d retains a carriage return after repeated clear: %q", index, row)
		}
	}
	oldest, _ := assetTUIMultiViewport(rows, 5, len(rows))
	if !strings.Contains(strings.Join(oldest, "\n"), "echo first") {
		t.Fatalf("oldest generation is unavailable while scrolling: %#v", oldest)
	}
	live, _ := assetTUIMultiViewport(rows, 5, 0)
	if !strings.Contains(strings.Join(live, "\n"), "echo third") {
		t.Fatalf("latest generation is unavailable after repeated clear: %#v", live)
	}
}

func TestAssetTUIMultiSessionPromptSurvivesClearAndRedraw(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(40, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()
	if _, err = screen.Write([]byte("prompt$ echo one\r\none\r\nprompt$ ")); err != nil {
		t.Fatal(err)
	}
	screen.PreserveNextClear()
	if _, err = screen.Write([]byte("\x1b[H\x1b[2Jprompt$ ")); err != nil {
		t.Fatal(err)
	}
	visible, _ := assetTUIMultiViewport(screen.ViewRows(6), 6, 0)
	if len(visible) == 0 || visible[0] != "prompt$" {
		t.Fatalf("prompt disappeared after clear and redraw: %#v", visible)
	}
}

func TestAssetTUIMultiSessionPromptStyleSurvivesClearAndRedraw(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(40, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()

	_, _ = screen.Write([]byte("\x1b[32mprompt$\x1b[0m echo one\r\none\r\n"))
	screen.PreserveNextClear()
	_, _ = screen.Write([]byte("\x1b[H\x1b[2J\x1b[32mprompt$\x1b[0m "))

	visible, _ := assetTUIMultiViewport(screen.RenderRows(6), 6, 0)
	rendered := strings.Join(visible, "\n")
	if !strings.Contains(rendered, "\x1b[") || !strings.Contains(rendered, "prompt$") {
		t.Fatalf("styled prompt was not preserved after clear and redraw: %q", rendered)
	}
}

func TestAssetTUIMultiSessionReuseNoticeKeepsCursorOnNextLine(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(200, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()

	_, _ = screen.Write([]byte("复用SSH连接（Koko TUI 测试账号@127.0.0.1）[连接数量: 2]\r\n"))
	x, y, cursorErr := screen.Cursor()
	if cursorErr != nil || x != 0 || y != 1 {
		t.Fatalf("reuse notice left the cursor at (%d,%d), want (0,1): %v", x, y, cursorErr)
	}

	// Shortcut mode paints the footer while keeping the session cursor hidden.
	// The hidden cursor must still be restored to the logical session position
	// before the next remote output arrives.
	_, _ = screen.Write([]byte("\x1b[20;1Hfooter"))
	_, _ = screen.Write([]byte(assetTUIMultiCursorRestore(200, 20, x, y, false)))
	_, _ = screen.Write([]byte("Last login: Thu Oct  1 02:11:13 2026 from 172.17.0.1\r\n" +
		"\x1b[32mkoko@koko-tui-linux\x1b[0m:~$ "))
	rendered := strings.Join(screen.RenderRows(20), "\n")
	for _, value := range []string{
		"复用SSH连接（Koko TUI 测试账号@127.0.0.1）[连接数量: 2]",
		"Last login: Thu Oct  1 02:11:13 2026 from 172.17.0.1",
		"koko@koko-tui-linux",
	} {
		if !strings.Contains(rendered, value) {
			t.Fatalf("duplicated session output lost %q: %q", value, rendered)
		}
	}
}

func TestAssetTUIMultiSessionScreenIsolation(t *testing.T) {
	first, err := newAssetTUIMultiScreen(40, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newAssetTUIMultiScreen(40, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	_, _ = first.Write([]byte("first-session"))
	first.PreserveNextClear()
	_, _ = first.Write([]byte("\x1b[H\x1b[2Jprompt\r\ncommand"))
	firstView := strings.Join(first.ViewRows(6), "\n")
	firstX, firstY, _ := first.Cursor()

	_, _ = second.Write([]byte("second-session\r\noutput"))
	if current := strings.Join(first.ViewRows(6), "\n"); current != firstView {
		t.Fatalf("another session changed the first session view: before=%q after=%q", firstView, current)
	}
	if x, y, _ := first.Cursor(); x != firstX || y != firstY {
		t.Fatalf("another session moved the first session cursor: before=(%d,%d) after=(%d,%d)",
			firstX, firstY, x, y)
	}
	secondView := strings.Join(second.ViewRows(6), "\n")
	if !strings.Contains(secondView, "second-session") || strings.Contains(secondView, "first-session") ||
		strings.Contains(secondView, "command") {
		t.Fatalf("session content leaked between independent screens: %q", secondView)
	}
}

func TestAssetTUIMultiSessionMouseScrollIsolation(t *testing.T) {
	first, err := newAssetTUIMultiScreen(40, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newAssetTUIMultiScreen(40, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	content := []byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\nseven\r\neight")
	_, _ = first.Write(content)
	_, _ = second.Write(content)

	manager := &assetTUIMultiSessionManager{
		width: 40, height: 6,
		sessions: []*assetTUIMultiSession{{screen: first}, {screen: second}},
	}
	wheelUp := assetTUIMultiMouseEvent{button: 64, y: 1, press: true}
	manager.handleMouse(wheelUp)
	if manager.sessions[0].scrollOffset != 0 {
		t.Fatalf("focused session still let Koko capture the mouse: offset=%d",
			manager.sessions[0].scrollOffset)
	}
	manager.commandMode = true
	manager.handleMouse(wheelUp)
	firstOffset := manager.sessions[0].scrollOffset
	if firstOffset == 0 || manager.sessions[1].scrollOffset != 0 {
		t.Fatalf("first session scroll leaked: first=%d second=%d",
			firstOffset, manager.sessions[1].scrollOffset)
	}
	manager.active = 1
	manager.handleMouse(wheelUp)
	if manager.sessions[0].scrollOffset != firstOffset || manager.sessions[1].scrollOffset == 0 {
		t.Fatalf("second session scroll changed the first: first=%d second=%d",
			manager.sessions[0].scrollOffset, manager.sessions[1].scrollOffset)
	}
}

func TestAssetTUIMultiSessionImmersiveMode(t *testing.T) {
	screen, err := newAssetTUIMultiScreen(80, 21)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()
	connection := &assetTUIMultiUserConnection{winch: make(chan ssh.Window, 1)}
	manager := &assetTUIMultiSessionManager{
		width: 80, height: 24, commandMode: true,
		sessions: []*assetTUIMultiSession{{screen: screen, conn: connection}},
	}
	if leave, commandErr := manager.handleCommand('z'); commandErr != nil || leave {
		t.Fatalf("z did not enter immersive mode: leave=%v err=%v", leave, commandErr)
	}
	if !manager.immersive || !manager.immersiveHint || manager.commandMode {
		t.Fatalf("unexpected immersive state: immersive=%v hint=%v command=%v",
			manager.immersive, manager.immersiveHint, manager.commandMode)
	}
	select {
	case window := <-connection.winch:
		if window.Height != 24 {
			t.Fatalf("immersive session height is %d, want 24", window.Height)
		}
	default:
		t.Fatal("immersive mode did not resize the session")
	}
	manager.exitImmersiveMode()
	if manager.immersive || manager.immersiveHint || !manager.commandMode {
		t.Fatalf("unexpected restored state: immersive=%v hint=%v command=%v",
			manager.immersive, manager.immersiveHint, manager.commandMode)
	}
	select {
	case window := <-connection.winch:
		if window.Height != 21 {
			t.Fatalf("restored session height is %d, want 21", window.Height)
		}
	default:
		t.Fatal("leaving immersive mode did not restore the session size")
	}
	manager.setCommandMode(false)
	select {
	case window := <-connection.winch:
		if window.Height != 22 {
			t.Fatalf("focused session height is %d, want 22", window.Height)
		}
	default:
		t.Fatal("entering the session did not expand its operation area")
	}
	manager.setCommandMode(true)
	select {
	case window := <-connection.winch:
		if window.Height != 21 {
			t.Fatalf("session-window height is %d, want 21", window.Height)
		}
	default:
		t.Fatal("activating the session window did not reserve the tab row")
	}
	if assetTUIMultiHintDuration != 5*time.Second {
		t.Fatalf("immersive hint duration is %s, want 5s", assetTUIMultiHintDuration)
	}
}

func TestAssetTUIMultiUserConnectionBuffersAndClosesInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	connection := &assetTUIMultiUserConnection{
		input: make(chan []byte, 1), ctx: ctx, cancel: cancel,
	}
	if err := connection.sendInput([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	if n, err := connection.Read(buffer); err != nil || string(buffer[:n]) != "hello" {
		t.Fatalf("unexpected buffered input: n=%d err=%v data=%q", n, err, buffer[:n])
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.sendInput([]byte("ignored")); err == nil {
		t.Fatal("closed multi-session input accepted more data")
	}
}

func TestAssetTUIConnectionChoicesPreferLastSelection(t *testing.T) {
	store := newTerminalPreferenceStore(t.TempDir())
	handler := &InteractiveHandler{user: &model.User{ID: t.Name()}, preferenceStore: store}
	asset := model.PermAsset{ID: "asset-1", OrgID: "org-1"}
	handler.saveLastConnectionPreference(asset, model.PermAccount{Alias: "account-1"}, "ssh")
	handler.saveLastConnectionPreference(asset, model.PermAccount{Alias: "account-2"}, "telnet")
	tui := assetTUI{handler: handler}
	accounts := []model.PermAccount{{Alias: "account-1"}, {Alias: "account-2"}, {Alias: "account-3"}}
	protocols := []string{"ssh", "telnet", "mysql"}
	tui.Update(assetChoicesMsg{asset: asset, accounts: accounts, protocols: protocols})
	if tui.dialog == nil || tui.dialog.accounts[0].Alias != "account-2" || tui.dialog.protocols[0] != "telnet" {
		t.Fatalf("last connection choices were not moved to the front: dialog=%#v", tui.dialog)
	}
	if tui.dialog.accounts[1].Alias != "account-1" || tui.dialog.accounts[2].Alias != "account-3" ||
		tui.dialog.protocols[1] != "ssh" || tui.dialog.protocols[2] != "mysql" {
		t.Fatalf("remaining connection choices changed order: accounts=%#v protocols=%#v",
			tui.dialog.accounts, tui.dialog.protocols)
	}

	tui.dialog = nil
	tui.connection = nil
	if _, cmd := tui.Update(assetChoicesMsg{
		asset:    model.PermAsset{ID: "asset-2"},
		accounts: []model.PermAccount{{Alias: "account-4"}}, protocols: []string{"ssh"},
	}); cmd == nil || tui.dialog != nil || tui.connection == nil {
		t.Fatalf("single account and protocol did not connect directly: dialog=%#v connection=%#v", tui.dialog, tui.connection)
	}
	if tui.connection.multiWindow {
		t.Fatal("native connection unexpectedly entered multi-session mode")
	}
}

func TestAssetTUIDialogVIKeyNavigation(t *testing.T) {
	tui := assetTUI{dialog: newAssetTUIDialog(
		model.PermAsset{},
		[]model.PermAccount{{Alias: "account-1"}, {Alias: "account-2"}},
		[]string{"ssh", "telnet"},
	)}
	for _, key := range []rune{'k', 'h'} {
		tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if tui.dialog.accountIndex != 0 || tui.dialog.protocolIndex != 0 {
		t.Fatalf("k/h moved beyond the first dialog choices: account=%d protocol=%d",
			tui.dialog.accountIndex, tui.dialog.protocolIndex)
	}
	for _, key := range []rune{'j', 'l'} {
		tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if tui.dialog.accountIndex != 1 || tui.dialog.protocolIndex != 1 {
		t.Fatalf("j/l did not move dialog selection: account=%d protocol=%d",
			tui.dialog.accountIndex, tui.dialog.protocolIndex)
	}
	for _, key := range []rune{'j', 'l'} {
		tui.updateDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if tui.dialog.accountIndex != 1 || tui.dialog.protocolIndex != 1 {
		t.Fatalf("j/l moved beyond the last dialog choices: account=%d protocol=%d",
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
		[]model.PermAccount{{Name: "Alice", Alias: "account-1"}, {Name: "Bob", Alias: "account-2"}},
		[]string{"ssh"},
	)
	dialog.accountSearch = []rune("bob")
	dialog.filterAccounts()
	dialog.searchingAccount = true
	tui := assetTUI{handler: &InteractiveHandler{i18nLang: "en"}, width: 80, height: 24, dialog: dialog}
	tui.updateAccountSearch(tea.KeyMsg{Type: tea.KeyCtrlH})
	if string(dialog.accountSearch) != "bo" {
		t.Fatalf("ctrl+h did not delete the previous account-search character: %q", dialog.accountSearch)
	}
	dialog.accountSearch = []rune("bob")
	dialog.filterAccounts()
	dialog.searchingAccount = false
	account, ok := dialog.selectedAccount()
	if !ok || account.Name != "Bob" {
		t.Fatalf("unexpected filtered account: %#v", account)
	}
	dialog.accountSearch = []rune("account-2")
	dialog.filterAccounts()
	if len(dialog.accountMatches) != 0 {
		t.Fatalf("account search unexpectedly matched an alias: %#v", dialog.accountMatches)
	}
	dialog.accountSearch = []rune("bob")
	dialog.filterAccounts()
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
	if !strings.Contains(lines[geometry.y+6], "│  Account · Search:bob") {
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
