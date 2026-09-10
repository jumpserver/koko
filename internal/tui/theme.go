package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// These colors belong to the xterm 256-color palette, so ordinary SSH clients
// keep the same neutral surfaces and restrained accents as truecolor clients.
var (
	Background       = tcell.NewHexColor(0x121212)
	Panel            = Background // All regions share the page background.
	Raised           = tcell.NewHexColor(0x262626)
	Foreground       = tcell.NewHexColor(0xd7d7d7)
	Muted            = tcell.NewHexColor(0x949494)
	Border           = tcell.NewHexColor(0x444444)
	Accent           = tcell.NewHexColor(0x87afaf)
	FocusBorder      = tcell.NewHexColor(0x5f8787)
	Selected         = tcell.StyleDefault.Foreground(Foreground).Background(tcell.NewHexColor(0x3a3a3a))
	InactiveSelected = tcell.StyleDefault.Foreground(Foreground).Background(Raised)
)

// RoundedBorder retains Box's title, padding and hit testing, while replacing
// its heavy double focus border. Styling stays local to each SSH session.
func RoundedBorder(box *tview.Box) {
	box.SetDrawFunc(func(s tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if width >= 2 && height >= 2 {
			style := tcell.StyleDefault.Foreground(box.GetBorderColor()).Background(box.GetBackgroundColor())
			for col := x + 1; col < x+width-1; col++ {
				for _, row := range []int{y, y + height - 1} {
					r, combining, cellStyle, _ := s.GetContent(col, row)
					if r == '═' {
						s.SetContent(col, row, '─', combining, cellStyle)
					}
				}
			}
			for row := y + 1; row < y+height-1; row++ {
				s.SetContent(x, row, '│', nil, style)
				s.SetContent(x+width-1, row, '│', nil, style)
			}
			// Cut the panel fill back at the corners as well as rounding the
			// glyphs, so the surface does not remain a solid square block.
			corner := style.Background(Background)
			s.SetContent(x, y, '╭', nil, corner)
			s.SetContent(x+width-1, y, '╮', nil, corner)
			s.SetContent(x, y+height-1, '╰', nil, corner)
			s.SetContent(x+width-1, y+height-1, '╯', nil, corner)
		}
		return box.GetInnerRect()
	})
}
