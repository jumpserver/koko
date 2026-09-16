package handler

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

type tuiChrome struct {
	help     help.Model
	bindings []key.Binding
}

func (c *tuiChrome) addHint(label, description string) {
	c.bindings = append(c.bindings, key.NewBinding(key.WithKeys(label), key.WithHelp(label, description)))
}

func (h *terminalUI) installCharmStyles() {
	h.chrome.help = help.New()
	h.chrome.help.ShortSeparator = "   "
	h.chrome.help.Styles.ShortKey = lipgloss.NewStyle().Foreground(tui.StyleColor(tui.Foreground)).Background(tui.StyleColor(tui.Raised)).Padding(0, 1)
	h.chrome.help.Styles.ShortDesc = lipgloss.NewStyle().Foreground(tui.StyleColor(tui.Muted))
	h.chrome.help.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(tui.StyleColor(tui.Border))
	h.chrome.help.Styles.Ellipsis = h.chrome.help.Styles.ShortDesc

	// An open header and solid active tabs establish hierarchy without putting
	// another competing border around every control.
	h.header.SetBorder(false).SetDrawFunc(nil).SetBorderPadding(1, 1, 1, 1)
	h.main.ResizeItem(h.tabRow, 1, 0)
	h.tabRow.SetBorderPadding(0, 0, 0, 0)
	h.header.SetDrawFunc(func(s tcell.Screen, x, y, w, height int) (int, int, int, int) {
		if height > 1 {
			style := tcell.StyleDefault.Foreground(tui.Border).Background(tui.Background)
			for col := x; col < x+w; col++ {
				s.SetContent(col, y+height-1, '─', nil, style)
			}
		}
		return x + 1, y + 1, max(0, w-2), max(0, height-2)
	})
	h.sessionTabs.SetSeparator(' ').SetBorderPadding(0, 0, 1, 1)
	h.brand.SetDrawFunc(func(s tcell.Screen, x, y, w, height int) (int, int, int, int) {
		ink := tui.Background
		if h.lightTheme {
			ink = tui.Foreground
		}
		badge := lipgloss.NewStyle().Foreground(tui.StyleColor(ink)).Background(tui.StyleColor(tui.Accent)).Bold(true).Padding(0, 1).Render("K")
		title := lipgloss.NewStyle().Foreground(tui.StyleColor(tui.Foreground)).Bold(true).Render(h.brand.GetText(true))
		tui.DrawStyled(s, lipgloss.NewStyle().MaxWidth(w).Render(badge+"  "+title), x, y, w, 1)
		return x, y, 0, height
	})
	h.footer.SetDrawFunc(func(s tcell.Screen, x, y, w, height int) (int, int, int, int) {
		h.chrome.help.SetWidth(max(0, w-2))
		tui.DrawStyled(s, h.chrome.help.ShortHelpView(h.chrome.bindings), x+1, y, max(0, w-2), 1)
		return x, y, 0, height
	})
}

func (h *terminalUI) drawCharmTabs(screen tcell.Screen) {
	_, selected := h.sessionTabs.GetSelection()
	for col := 0; col < h.sessionTabs.GetColumnCount(); col++ {
		cell := h.sessionTabs.GetCell(0, col)
		x, y, width := cell.GetLastPosition()
		if row, visible := h.sessionTabs.CellAt(x, y); row != 0 || visible != col || width <= 0 {
			continue
		}
		style := lipgloss.NewStyle().Foreground(tui.StyleColor(tui.Muted)).Background(tui.StyleColor(tui.Panel))
		if col == h.activeSession+1 {
			ink := tui.Background
			if h.lightTheme {
				ink = tui.Foreground
			}
			style = style.Foreground(tui.StyleColor(ink)).Background(tui.StyleColor(tui.Accent)).Bold(true)
		} else if h.sessionTabs.HasFocus() && selected == col {
			style = style.Foreground(tui.StyleColor(tui.Foreground)).Background(tui.StyleColor(tui.Raised))
		}
		label := strings.TrimSpace(tview.Unescape(h.sessionTabLabel(col - 1)))
		padding := ""
		if col > 0 {
			padding = " "
		}
		if !h.modal && h.windowPrefix || h.hintsVisible() && h.activeSession < 0 && col > 0 {
			label = padding + fmt.Sprint(col) + " " + label + " "
		} else {
			label = padding + label + " "
		}
		tui.DrawStyled(screen, style.Width(width).MaxWidth(width).Render(label), x, y, width, 1)
	}
}
