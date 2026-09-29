package handler

import (
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

func TestAssetTUIDialogCoversTwoCellsAroundBorder(t *testing.T) {
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
	maskX := geometry.x - 2
	maskRight := geometry.x + geometry.width + 2
	line := lines[geometry.y]
	if !strings.HasPrefix(line, strings.Repeat(".", maskX)) ||
		!strings.HasSuffix(line, strings.Repeat(".", tui.width-maskRight)) ||
		!strings.Contains(line, "  ┌") {
		t.Fatalf("dialog side mask has unexpected bounds: %q", line)
	}
	for _, row := range []int{geometry.y - 2, geometry.y - 1, geometry.y + geometry.height, geometry.y + geometry.height + 1} {
		masked := lines[row]
		if masked[maskX:maskRight] != strings.Repeat(" ", maskRight-maskX) {
			t.Fatalf("dialog row %d was not masked: %q", row, masked)
		}
	}
}
