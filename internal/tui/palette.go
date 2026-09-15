package tui

import (
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
)

type Palette struct {
	Background, Foreground, Muted, Border  tcell.Color
	Accent, FocusBorder, Raised, Selection tcell.Color
	ansiAccent                             tcell.Color
}

func ThemePalette(light bool, accent int) Palette {
	p := Palette{Background, Foreground, Muted, Border, Accent, FocusBorder, Raised, tcell.NewHexColor(0x3a3a3a), tcell.ColorDefault}
	accents := [][2]int32{{0x187f63, 0x146b53}, {0x5f87ff, 0x5f87af}, {0x875fff, 0x875faf}, {0xffaf00, 0xaf8700}, {0xff5faf, 0xaf5f87}, {0x00afd7, 0x0087af}, {0x5f8787, 0x5f5f5f}, {0xff5f5f, 0xaf5f5f}, {0x87d700, 0x5f8700}}
	if light {
		p.Background, p.Foreground = tcell.NewHexColor(0xffffff), tcell.NewHexColor(0x303030)
		p.Muted, p.Border = tcell.NewHexColor(0x5f5f5f), tcell.NewHexColor(0x585858)
		p.Raised, p.Selection = tcell.NewHexColor(0xdadada), tcell.NewHexColor(0xd7d7d7)
		accents = [][2]int32{{0x187f63, 0x146b53}, {0x005faf, 0x5f87af}, {0x5f00af, 0x875faf}, {0xaf5f00, 0xd78700}, {0xaf005f, 0xaf5f87}, {0x005f87, 0x0087af}, {0x444444, 0x878787}, {0xaf0000, 0xd75f5f}, {0x5f8700, 0x87af00}}
	}
	index := max(0, min(accent, len(accents)-1))
	colors := accents[index]
	p.Accent, p.FocusBorder = tcell.NewHexColor(colors[0]), tcell.NewHexColor(colors[1])
	p.ansiAccent = [...]tcell.Color{
		tcell.ColorTeal, tcell.ColorNavy, tcell.ColorPurple,
		tcell.ColorOlive, tcell.ColorPurple, tcell.ColorTeal,
		tcell.ColorTeal, tcell.ColorMaroon, tcell.ColorGreen,
	}[index]
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
	syncPending      atomic.Bool
	frozen           atomic.Bool
	passthroughMu    sync.Mutex
	passthroughOwner *Terminal
}

type passthroughScreen interface {
	beginPassthrough(func([]byte) bool) bool
	endPassthrough()
	writePassthrough([]byte) (int, error)
}

func NewThemeScreen(screen tcell.Screen) *ThemeScreen {
	s := &ThemeScreen{Screen: screen}
	s.SetPalette(ThemePalette(false, 0))
	return s
}

var ansi8Palette = []tcell.Color{
	tcell.ColorBlack, tcell.ColorMaroon, tcell.ColorGreen, tcell.ColorOlive,
	tcell.ColorNavy, tcell.ColorPurple, tcell.ColorTeal, tcell.ColorSilver,
}

func adaptLowColorPalette(p Palette, colors int) Palette {
	if colors <= 0 || colors > len(ansi8Palette) {
		return p
	}
	background := tcell.FindColor(p.Background, ansi8Palette)
	if background == tcell.ColorBlack {
		p.Background, p.Border = tcell.ColorBlack, tcell.ColorSilver
	} else {
		p.Background, p.Border = tcell.ColorSilver, tcell.ColorBlack
	}
	if p.ansiAccent.Valid() {
		p.Accent, p.FocusBorder = p.ansiAccent, p.ansiAccent
	}
	return p
}

func (s *ThemeScreen) SetPalette(p Palette) {
	p = adaptLowColorPalette(p, s.Screen.Colors())
	s.forward = map[tcell.Color]tcell.Color{
		Background: p.Background, Foreground: p.Foreground, Muted: p.Muted,
		Border: p.Border, Accent: p.Accent, FocusBorder: p.FocusBorder,
		Raised: p.Raised, tcell.NewHexColor(0x3a3a3a): p.Selection,
	}
	s.reverse = make(map[tcell.Color]tcell.Color, len(s.forward))
	for base, color := range s.forward {
		s.reverse[color] = base
	}
	// Sync clears the terminal before repainting. Use the current palette
	// rather than exposing the client's default (often white) background.
	s.Screen.SetStyle(tcell.StyleDefault.Foreground(p.Foreground).Background(p.Background))
}

// RequestSync repairs client-side IME damage after widgets finish drawing.
// Repeated requests are coalesced into one complete frame on the UI loop.
func (s *ThemeScreen) RequestSync() { s.syncPending.Store(true) }

func (s *ThemeScreen) Show() {
	s.passthroughMu.Lock()
	defer s.passthroughMu.Unlock()
	if s.frozen.Load() {
		return
	}
	if s.syncPending.Swap(false) {
		s.Screen.Sync()
		return
	}
	s.Screen.Show()
}

func (s *ThemeScreen) Sync() {
	s.passthroughMu.Lock()
	defer s.passthroughMu.Unlock()
	if !s.frozen.Load() {
		s.Screen.Sync()
	}
}

// BeginPassthrough temporarily gives one embedded terminal the raw SSH byte
// stream. Protocols such as ZMODEM cannot survive the cell-based TUI renderer.
func (s *ThemeScreen) BeginPassthrough(owner *Terminal, forward func([]byte) bool) bool {
	raw, ok := s.Screen.(passthroughScreen)
	if !ok || owner == nil || forward == nil {
		return false
	}
	s.passthroughMu.Lock()
	defer s.passthroughMu.Unlock()
	if s.passthroughOwner != nil {
		return s.passthroughOwner == owner
	}
	s.frozen.Store(true)
	if !raw.beginPassthrough(forward) {
		s.frozen.Store(false)
		return false
	}
	s.passthroughOwner = owner
	return true
}

func (s *ThemeScreen) WritePassthrough(owner *Terminal, p []byte) (int, error) {
	s.passthroughMu.Lock()
	defer s.passthroughMu.Unlock()
	if s.passthroughOwner != owner {
		return 0, nil
	}
	return s.Screen.(passthroughScreen).writePassthrough(p)
}

func (s *ThemeScreen) EndPassthrough(owner *Terminal) {
	s.passthroughMu.Lock()
	defer s.passthroughMu.Unlock()
	if s.passthroughOwner != owner {
		return
	}
	s.Screen.(passthroughScreen).endPassthrough()
	s.passthroughOwner = nil
	s.syncPending.Store(true)
	s.frozen.Store(false)
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
