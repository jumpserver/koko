package tui

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
)

// StyleColor uses semantic colors so Lip Gloss views follow the same per-login
// palette and low-color terminal fallback as the embedded terminal.
func StyleColor(c tcell.Color) color.Color {
	r, g, b := c.RGB()
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

// DrawStyled translates Lip Gloss output into the existing SSH cell buffer.
// ANSI is decoded locally; styled UI strings never write to the SSH stream.
func DrawStyled(screen tcell.Screen, text string, x, y, width, height int) {
	sw, sh := screen.Size()
	width, height = min(width, sw-x), min(height, sh-y)
	if width <= 0 || height <= 0 {
		return
	}
	buffer := uv.NewScreenBuffer(width, height)
	uv.NewStyledString(text).Draw(buffer, buffer.Bounds())
	toColor := func(c color.Color, fallback tcell.Color) tcell.Color {
		if c == nil {
			return fallback
		}
		r, g, b, _ := c.RGBA()
		return tcell.NewRGBColor(int32(r>>8), int32(g>>8), int32(b>>8))
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; {
			cell := buffer.CellAt(col, row)
			if cell == nil || cell.Width == 0 {
				col++
				continue
			}
			style := tcell.StyleDefault.Foreground(toColor(cell.Style.Fg, Foreground)).Background(toColor(cell.Style.Bg, Panel)).
				Bold(cell.Style.Attrs&uv.AttrBold != 0).Dim(cell.Style.Attrs&uv.AttrFaint != 0).
				Italic(cell.Style.Attrs&uv.AttrItalic != 0).Reverse(cell.Style.Attrs&uv.AttrReverse != 0).
				Underline(cell.Style.Underline != uv.UnderlineNone)
			runes := []rune(cell.Content)
			if len(runes) == 0 {
				runes = []rune{' '}
			}
			if col+cell.Width <= width {
				screen.SetContent(x+col, y+row, runes[0], runes[1:], style)
			}
			col += max(1, cell.Width)
		}
	}
}
