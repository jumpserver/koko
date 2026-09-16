package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/tui"
	"github.com/rivo/tview"
)

func TestCharmWorkspaceLayout(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {72, 20}} {
		for _, light := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/light=%v", size[0], size[1], light), func(t *testing.T) {
				screen := tcell.NewSimulationScreen("UTF-8")
				if err := screen.Init(); err != nil {
					t.Fatal(err)
				}
				defer screen.Fini()
				screen.SetSize(size[0], size[1])
				theme := tui.NewThemeScreen(screen)
				theme.SetPalette(tui.ThemePalette(light, 0))
				h := &terminalUI{screen: screen, themeScreen: theme, app: tui.NewApplication().SetScreen(theme),
					ctx: context.Background(), user: &model.User{}, data: tuiData{lang: "zh"},
					activeSession: -1, sidebarWidth: 32, lightTheme: light, pages: tui.NewPages()}
				h.build()
				h.installCharmStyles()
				h.app.SetRoot(h.main, true).SetFocus(h.table)
				h.renderAssets()
				h.status.SetText("")
				h.main.SetRect(0, 0, size[0], size[1])
				h.updateLayout()
				h.setWindowHelp()
				h.main.Draw(theme)
				h.drawSessionTabs(theme)
				buttonX, buttonY, buttonWidth, _ := h.assetRefresh.GetRect()
				if r, _, _, _ := screen.GetContent(buttonX+buttonWidth/2-1, buttonY+1); r != []rune(tuiRefreshIcon)[0] {
					t.Fatalf("refresh icon is missing its optical offset: %q", r)
				}
				_, _, width, height := h.assetPane.GetInnerRect()
				if width < 20 || height < 5 {
					t.Fatalf("asset viewport too small: %dx%d", width, height)
				}
				if r, _, _, _ := screen.GetContent(3, 1); r != 'K' {
					t.Fatalf("missing Lip Gloss header: %q", r)
				}
				if r, _, _, _ := screen.GetContent(2, size[1]-1); r != '?' {
					t.Fatalf("missing Bubbles help: %q", r)
				}
				h.accountDialog(model.PermAsset{}, []model.PermAccount{{Username: "alpha"}, {Username: "beta"}, {Username: "gamma"}}, []string{"ssh", "sftp"})
				h.openFocusedDropdown()
				for range 2 {
					h.main.Draw(theme)
					h.drawDropdown(theme)
					picker := h.dialogs[0].accountSearch.picker
					picker.Focus(func(p tview.Primitive) {
						list := p.(*tui.List)
						x, y, _, _ := list.GetInnerRect()
						for row, want := range []rune{'a', 'b', 'g'} {
							if r, _, _, _ := screen.GetContent(x+1, y+row); r != want {
								t.Errorf("account option %d covered: got %q, want %q", row, r, want)
							}
						}
					})
				}
				search := h.dialogs[0].accountSearch
				search.field.PasteHandler()("alpha", func(tview.Primitive) {})
				h.main.Draw(theme)
				h.drawDropdown(theme)
				x, y, _, _ := search.field.GetInnerRect()
				x += tview.TaggedStringWidth(search.field.GetLabel())
				for col, want := range "alpha" {
					if r, _, _, _ := screen.GetContent(x+col, y); r != want {
						t.Errorf("account search column %d: got %q, want %q", col, r, want)
					}
				}

			})
		}
	}
}

func TestShortcutHintMode(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(120, 32)
	theme := tui.NewThemeScreen(screen)
	h := &terminalUI{screen: screen, themeScreen: theme, app: tui.NewApplication().SetScreen(theme),
		ctx: context.Background(), user: &model.User{}, data: tuiData{lang: "zh"},
		activeSession: -1, sidebarWidth: 32, pages: tui.NewPages()}
	h.build()
	h.app.SetRoot(h.main, true).SetFocus(h.table)
	h.setWindowHelp()
	hidden := h.assetRefresh.GetLabel()
	if !strings.HasPrefix(h.search.GetLabel(), "/ ") {
		t.Fatal("search shortcut should remain visible without hint mode")
	}
	if strings.Contains(hidden, "r") || h.hintsVisible() {
		t.Fatal("hints should be hidden by default")
	}
	question := tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone)
	h.input(question)
	h.setWindowHelp()
	if !h.hintsVisible() || !strings.Contains(h.assetRefresh.GetLabel(), "r") ||
		tview.TaggedStringWidth(hidden)+2 != tview.TaggedStringWidth(h.assetRefresh.GetLabel()) {
		t.Fatal("hidden hints must not reserve button space")
	}
	h.input(question)
	if h.hintsVisible() {
		t.Fatal("? should hide hints")
	}
	h.input(question)
	h.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if h.hintsVisible() || !h.table.HasFocus() {
		t.Fatal("Esc should only dismiss hints")
	}
	h.input(question)
	h.input(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	if h.hintsVisible() || !h.search.HasFocus() {
		t.Fatal("executing a shortcut should dismiss hints")
	}
	if h.input(question) != question {
		t.Fatal("? must reach the search input")
	}
	h.input(tcell.NewEventKey(tcell.KeyF10, 0, 0))
	if !h.modal || h.dialogs[len(h.dialogs)-1].page != "help" {
		t.Fatal("F10 must open help while editing")
	}
	h.dismissModal()
	terminal, err := tui.NewTerminal(context.Background(), func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	h.popup = terminal
	h.sessions = []*tuiSession{{terminal: terminal}}
	h.activeSession = 0
	h.app.SetFocus(terminal)
	for _, event := range []*tcell.EventKey{question, tcell.NewEventKey(tcell.KeyF10, 0, 0)} {
		h.input(event)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		buffer := make([]byte, 32)
		n, err := terminal.ReadContext(ctx, buffer)
		cancel()
		if err != nil || n == 0 || h.modal || h.hintsVisible() {
			t.Fatal("remote input must reach the terminal")
		}
	}
	h.input(tcell.NewEventKey(tcell.KeyCtrlRightSq, 0, 0))
	if !h.hintsVisible() {
		t.Fatal("window prefix should reveal hints")
	}
	h.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if h.hintsVisible() {
		t.Fatal("Esc should leave window mode")
	}
}
