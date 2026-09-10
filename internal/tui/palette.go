package tui

import "github.com/gdamore/tcell/v2"

type Palette struct {
	Background, Foreground, Muted, Border  tcell.Color
	Accent, FocusBorder, Raised, Selection tcell.Color
}

func ThemePalette(light bool, accent int) Palette {
	p := Palette{Background, Foreground, Muted, Border, Accent, FocusBorder, Raised, tcell.NewHexColor(0x3a3a3a)}
	accents := [][2]int32{{0x187f63, 0x146b53}, {0x5f87ff, 0x5f87af}, {0x875fff, 0x875faf}, {0xffaf00, 0xaf8700}, {0xff5faf, 0xaf5f87}, {0x00afd7, 0x0087af}, {0x5f8787, 0x5f5f5f}, {0xff5f5f, 0xaf5f5f}, {0x87d700, 0x5f8700}}
	if light {
		p.Background, p.Foreground = tcell.NewHexColor(0xeeeeee), tcell.NewHexColor(0x303030)
		p.Muted, p.Border = tcell.NewHexColor(0x5f5f5f), tcell.NewHexColor(0xb2b2b2)
		p.Raised, p.Selection = tcell.NewHexColor(0xdadada), tcell.NewHexColor(0xd7d7d7)
		accents = [][2]int32{{0x187f63, 0x146b53}, {0x005faf, 0x5f87af}, {0x5f00af, 0x875faf}, {0xaf5f00, 0xd78700}, {0xaf005f, 0xaf5f87}, {0x005f87, 0x0087af}, {0x444444, 0x878787}, {0xaf0000, 0xd75f5f}, {0x5f8700, 0x87af00}}
	}
	colors := accents[max(0, min(accent, len(accents)-1))]
	p.Accent, p.FocusBorder = tcell.NewHexColor(colors[0]), tcell.NewHexColor(colors[1])
	return p
}

func AccentSwatch(index int) tcell.Color {
	colors := [...]int32{0x187f63, 0x3b82f6, 0x8b5cf6, 0xf59e0b, 0xec4899, 0x06b6d4, 0x64748b, 0xf43f5e, 0x84cc16}
	return tcell.NewHexColor(colors[max(0, min(index, len(colors)-1))])
}

// Each SSH screen owns its palette. Widgets retain semantic base colors, so
// existing dialogs, selections and scroll positions survive a live theme change.
// Palette changes and drawing both run on the application's UI event loop.
type ThemeScreen struct {
	tcell.Screen
	forward, reverse map[tcell.Color]tcell.Color
}

func NewThemeScreen(screen tcell.Screen) *ThemeScreen {
	s := &ThemeScreen{Screen: screen}
	s.SetPalette(ThemePalette(false, 0))
	return s
}

func (s *ThemeScreen) SetPalette(p Palette) {
	s.forward = map[tcell.Color]tcell.Color{
		Background: p.Background, Foreground: p.Foreground, Muted: p.Muted,
		Border: p.Border, Accent: p.Accent, FocusBorder: p.FocusBorder,
		Raised: p.Raised, tcell.NewHexColor(0x3a3a3a): p.Selection,
	}
	s.reverse = make(map[tcell.Color]tcell.Color, len(s.forward))
	for base, color := range s.forward {
		s.reverse[color] = base
	}
}

func paletteStyle(style tcell.Style, colors map[tcell.Color]tcell.Color) tcell.Style {
	fg, bg, _ := style.Decompose()
	if color, ok := colors[fg]; ok {
		style = style.Foreground(color)
	}
	if color, ok := colors[bg]; ok {
		style = style.Background(color)
	}
	return style
}

func (s *ThemeScreen) SetContent(x, y int, main rune, combining []rune, style tcell.Style) {
	s.Screen.SetContent(x, y, main, combining, paletteStyle(style, s.forward))
}

func (s *ThemeScreen) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	r, combining, style, width := s.Screen.GetContent(x, y)
	return r, combining, paletteStyle(style, s.reverse), width
}

func (s *ThemeScreen) SetStyle(style tcell.Style) {
	s.Screen.SetStyle(paletteStyle(style, s.forward))
}

func (s *ThemeScreen) Fill(r rune, style tcell.Style) {
	s.Screen.Fill(r, paletteStyle(style, s.forward))
}
