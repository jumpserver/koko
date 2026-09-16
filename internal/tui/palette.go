package tui

import (
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
)

type Palette struct {
	Background, Foreground, Muted, Disabled, DisabledSurface, Border tcell.Color
	Accent, FocusBorder, Raised, Selection                           tcell.Color
	ansiAccent                                                       tcell.Color
}

type ColorProfile int

const (
	ColorProfileMac ColorProfile = iota
	ColorProfileXShell
)

var macDarkThemeAccents = [][2]int32{
	{0x1ea782, 0x1ea782}, {0x60a5fa, 0x3b82f6}, {0xa78bfa, 0x8b5cf6},
	{0xfb923c, 0xf97316}, {0xf472b6, 0xec4899}, {0x22d3ee, 0x06b6d4},
	{0x94a3b8, 0x64748b}, {0xf87171, 0xef4444}, {0xa3e635, 0x84cc16},
}

var macLightThemeAccents = [][2]int32{
	{0x1ea782, 0x1ea782}, {0x1d4ed8, 0x1d4ed8}, {0x6d28d9, 0x6d28d9},
	{0xc2410c, 0xc2410c}, {0xbe185d, 0xbe185d}, {0x0e7490, 0x0e7490},
	{0x475569, 0x475569}, {0xb91c1c, 0xb91c1c}, {0x4d7c0f, 0x4d7c0f},
}

var xshellDarkThemeAccents = [][2]int32{
	{0x1ea782, 0x1ea782}, {0x6ea8fe, 0x4c8ee8}, {0xb197fc, 0x9775e6},
	{0xff9f43, 0xe98528}, {0xf783ac, 0xdc6892}, {0x3bc9db, 0x20adbf},
	{0xadb5bd, 0x868e96}, {0xff6b6b, 0xe55252}, {0x94d82d, 0x74b816},
}

var xshellLightThemeAccents = [][2]int32{
	{0x1ea782, 0x1ea782}, {0x2457a6, 0x2457a6}, {0x6f42c1, 0x6f42c1},
	{0xb54708, 0xb54708}, {0xad1457, 0xad1457}, {0x007c91, 0x007c91},
	{0x3f4d5a, 0x3f4d5a}, {0xb42318, 0xb42318}, {0x4f6f00, 0x4f6f00},
}

var darkANSIAccents = []tcell.Color{
	tcell.ColorTeal, tcell.ColorNavy, tcell.ColorPurple,
	tcell.ColorMaroon, tcell.ColorPurple, tcell.ColorTeal,
	tcell.ColorSilver, tcell.ColorMaroon, tcell.ColorGreen,
}

var lightANSIAccents = []tcell.Color{
	tcell.ColorTeal, tcell.ColorNavy, tcell.ColorPurple,
	tcell.ColorMaroon, tcell.ColorPurple, tcell.ColorTeal,
	tcell.ColorBlack, tcell.ColorMaroon, tcell.ColorGreen,
}

func ThemePalette(light bool, accent int) Palette {
	return ThemePaletteForProfile(light, accent, ColorProfileMac)
}

func ThemePaletteForProfile(light bool, accent int, profile ColorProfile) Palette {
	p := Palette{
		Background: Background, Foreground: Foreground, Muted: Muted, Disabled: Disabled,
		DisabledSurface: DisabledSurface, Border: Border,
		Accent: Accent, FocusBorder: FocusBorder, Raised: Raised, Selection: tcell.NewHexColor(0x3a3a3a),
		ansiAccent: tcell.ColorDefault,
	}
	accents, ansiAccents := macDarkThemeAccents, darkANSIAccents
	if profile == ColorProfileXShell {
		accents = xshellDarkThemeAccents
	}
	if light {
		p.Background, p.Foreground = tcell.NewHexColor(0xffffff), tcell.NewHexColor(0x1c1c1c)
		p.Muted, p.Disabled, p.DisabledSurface = tcell.NewHexColor(0x000000), tcell.NewHexColor(0x5f6368), tcell.NewHexColor(0xf1f3f4)
		p.Border = tcell.NewHexColor(0x585858)
		p.Raised, p.Selection = tcell.NewHexColor(0xdadada), tcell.NewHexColor(0xd7d7d7)
		accents, ansiAccents = macLightThemeAccents, lightANSIAccents
		if profile == ColorProfileXShell {
			accents = xshellLightThemeAccents
			p.Disabled, p.DisabledSurface = tcell.NewHexColor(0x4f5963), tcell.NewHexColor(0xe9edf1)
		}
	}
	index := max(0, min(accent, len(accents)-1))
	colors := accents[index]
	p.Accent, p.FocusBorder = tcell.NewHexColor(colors[0]), tcell.NewHexColor(colors[1])
	p.ansiAccent = ansiAccents[index]
	return p
}

func AccentSwatch(profile ColorProfile, light bool, index, terminalColors int) tcell.Color {
	return adaptLowColorPalette(ThemePaletteForProfile(light, index, profile), terminalColors).Accent
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
		p.Foreground, p.Muted, p.Disabled = tcell.ColorBlack, tcell.ColorBlack, tcell.ColorBlack
		p.DisabledSurface = p.Background
	}
	if p.ansiAccent.Valid() {
		p.Accent, p.FocusBorder = p.ansiAccent, p.ansiAccent
	}
	return p
}

func (s *ThemeScreen) SetPalette(p Palette) {
	p = adaptLowColorPalette(p, s.Screen.Colors())
	s.forward = map[tcell.Color]tcell.Color{
		Background: p.Background, Foreground: p.Foreground, Muted: p.Muted, Disabled: p.Disabled,
		DisabledSurface: p.DisabledSurface,
		Border:          p.Border, Accent: p.Accent, FocusBorder: p.FocusBorder,
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
