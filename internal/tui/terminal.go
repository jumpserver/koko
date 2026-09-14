package tui

import (
	"context"
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/gliderlabs/ssh"
	"github.com/rivo/tview"
	ghostty "go.mitchellh.com/libghostty"
)

const maxInputBytes = 64 * 1024

// Terminal renders remote escape sequences into cells, never into the outer SSH
// screen. Its stream and lifetime belong to one asset, not the login session.
type Terminal struct {
	*tview.Box
	mu            sync.Mutex
	vt            *ghostty.Terminal
	state         *ghostty.RenderState
	rows          *ghostty.RenderStateRowIterator
	cells         *ghostty.RenderStateRowCells
	keys          *ghostty.KeyEncoder
	mouse         *ghostty.MouseEncoder
	width, height int
	ctx           context.Context
	cancel        context.CancelFunc
	inputMu       sync.Mutex
	input         []byte
	ready         chan struct{}
	winch         chan ssh.Window
	invalidate    func()
}

func NewTerminal(ctx context.Context, invalidate func()) (t *Terminal, err error) {
	ctx, cancel := context.WithCancel(ctx)
	t = &Terminal{Box: tview.NewBox(), ctx: ctx, cancel: cancel,
		ready: make(chan struct{}, 1), winch: make(chan ssh.Window, 1), invalidate: invalidate,
		width: 80, height: 24}
	t.SetBackgroundColor(Panel)
	t.SetBorder(true).SetBorderColor(FocusBorder).SetTitleColor(Accent).SetBorderPadding(1, 0, 1, 0)
	RoundedBorder(t.Box)
	defer func() {
		if err != nil {
			t.Dispose()
		}
	}()
	t.vt, err = ghostty.NewTerminal(ghostty.WithSize(80, 24), ghostty.WithMaxScrollback(200),
		ghostty.WithWritePty(func(_ *ghostty.Terminal, b []byte) { t.SendInput(b) }))
	if err != nil {
		return
	}
	for color, setter := range map[tcell.Color]func(*ghostty.ColorRGB) error{
		Foreground: t.vt.SetColorForeground, Panel: t.vt.SetColorBackground,
	} {
		r, g, b := color.RGB()
		if err = setter(&ghostty.ColorRGB{R: uint8(r), G: uint8(g), B: uint8(b)}); err != nil {
			return
		}
	}
	// A text-only remote display must not read image files on the Koko host.
	zero := uint64(0)
	if err = t.vt.SetKittyImageStorageLimit(&zero); err != nil {
		return
	}
	if err = t.vt.SetKittyImageMediumFile(false); err != nil {
		return
	}
	if err = t.vt.SetKittyImageMediumTempFile(false); err != nil {
		return
	}
	if err = t.vt.SetKittyImageMediumSharedMem(false); err != nil {
		return
	}
	if t.state, err = ghostty.NewRenderState(); err != nil {
		return
	}
	if t.rows, err = ghostty.NewRenderStateRowIterator(); err != nil {
		return
	}
	if t.cells, err = ghostty.NewRenderStateRowCells(); err != nil {
		return
	}
	if t.keys, err = ghostty.NewKeyEncoder(); err != nil {
		return
	}
	t.mouse, err = ghostty.NewMouseEncoder()
	return
}

func (t *Terminal) Context() context.Context { return t.ctx }

// Only change the terminal defaults; explicit remote ANSI colors stay intact.
func (t *Terminal) SetPalette(p Palette) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vt == nil {
		return nil
	}
	for color, setter := range map[tcell.Color]func(*ghostty.ColorRGB) error{
		p.Foreground: t.vt.SetColorForeground, p.Background: t.vt.SetColorBackground,
	} {
		r, g, b := color.RGB()
		if err := setter(&ghostty.ColorRGB{R: uint8(r), G: uint8(g), B: uint8(b)}); err != nil {
			return err
		}
	}
	return nil
}
func (t *Terminal) WinCh() <-chan ssh.Window { return t.winch }
func (t *Terminal) Window() ssh.Window {
	t.mu.Lock()
	defer t.mu.Unlock()
	return ssh.Window{Width: t.width, Height: t.height}
}
func (t *Terminal) Close() error { t.cancel(); return nil }

// Dispose releases native resources even if a cancelled API call is still
// returning. Subsequent writes fail; a closed display is never reused.
func (t *Terminal) Dispose() {
	_ = t.Close()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mouse != nil {
		t.mouse.Close()
		t.mouse = nil
	}
	if t.keys != nil {
		t.keys.Close()
		t.keys = nil
	}
	if t.cells != nil {
		t.cells.Close()
		t.cells = nil
	}
	if t.rows != nil {
		t.rows.Close()
		t.rows = nil
	}
	if t.state != nil {
		t.state.Close()
		t.state = nil
	}
	if t.vt != nil {
		t.vt.Close()
		t.vt = nil
	}
	t.inputMu.Lock()
	clear(t.input)
	t.input = nil
	t.inputMu.Unlock()
}

func (t *Terminal) Read(p []byte) (int, error) {
	n, err := t.ReadContext(t.ctx, p)
	// The two cancellation cases in ReadContext may become ready together.
	// Closing this stream always reports EOF to its consumer.
	if err != nil && t.ctx.Err() != nil {
		return n, io.EOF
	}
	return n, err
}

// ReadContext lets an interactive connection step stop its reader without
// closing the asset session that will receive the subsequent shell input.
func (t *Terminal) ReadContext(ctx context.Context, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if t.ctx.Err() != nil {
			return 0, io.EOF
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		t.inputMu.Lock()
		if len(t.input) > 0 {
			n := copy(p, t.input)
			copy(t.input, t.input[n:])
			clear(t.input[len(t.input)-n:])
			t.input = t.input[:len(t.input)-n]
			t.inputMu.Unlock()
			return n, nil
		}
		t.inputMu.Unlock()
		select {
		case <-t.ready:
		case <-t.ctx.Done():
			return 0, io.EOF
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// SendInput never blocks the UI or the emulator callback. On overflow, cancel
// the session instead of sending a silently truncated command or password.
func (t *Terminal) SendInput(p []byte) bool {
	if t.ctx.Err() != nil {
		return false
	}
	t.inputMu.Lock()
	if len(p) > maxInputBytes-len(t.input) {
		t.inputMu.Unlock()
		t.cancel()
		return false
	}
	t.input = append(t.input, p...)
	t.inputMu.Unlock()
	select {
	case t.ready <- struct{}{}:
	default:
	}
	return true
}

func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vt == nil || t.ctx.Err() != nil {
		return 0, io.ErrClosedPipe
	}
	n, err := t.vt.Write(p)
	if t.invalidate != nil {
		t.invalidate()
	}
	return n, err
}

func (t *Terminal) SetRect(x, y, width, height int) {
	t.Box.SetRect(x, y, width, height)
	_, _, width, height = t.GetInnerRect()
	width, height = min(500, max(1, width)), min(200, max(1, height))
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vt == nil || width == t.width && height == t.height {
		return
	}
	if err := t.vt.Resize(uint16(width), uint16(height), 1, 1); err != nil {
		t.cancel()
		return
	}
	t.width, t.height = width, height
	select {
	case <-t.winch:
	default:
	}
	t.winch <- ssh.Window{Width: width, Height: height}
}

func rgb(c ghostty.ColorRGB) tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

func (t *Terminal) Draw(screen tcell.Screen) {
	t.Box.DrawForSubclass(screen, t)
	if themed, ok := screen.(*ThemeScreen); ok {
		screen = themed.Screen
	}
	x, y, width, height := t.GetInnerRect()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vt == nil || t.state.Update(t.vt) != nil || t.state.RowIterator(t.rows) != nil {
		return
	}
	fg, _ := t.state.ColorForeground()
	bg, _ := t.state.ColorBackground()
	base := tcell.StyleDefault.Foreground(rgb(fg)).Background(rgb(bg))
	var glyphs []uint32
	var styling ghostty.RenderCellStyle
	var combining []rune
	for row := 0; row < height && t.rows.Next(); row++ {
		if t.rows.Cells(t.cells) != nil {
			break
		}
		for col := 0; col < width; col++ {
			if t.cells.Select(uint16(col)) != nil {
				break
			}
			cell, err := t.cells.Raw()
			if err != nil {
				continue
			}
			wide, _ := cell.Wide()
			if wide == ghostty.CellWideSpacerTail {
				continue
			}
			style := base
			if t.cells.StyleInto(&styling) == nil {
				if styling.HasForeground {
					style = style.Foreground(rgb(styling.Foreground))
				}
				if styling.HasBackground {
					style = style.Background(rgb(styling.Background))
				}
				style = style.Bold(styling.Bold).Dim(styling.Faint).Italic(styling.Italic).
					Underline(styling.Underline).StrikeThrough(styling.Strikethrough).Reverse(styling.Inverse)
			}
			glyphs, _ = t.cells.GraphemesInto(glyphs[:0])
			r := ' '
			combining = combining[:0]
			if len(glyphs) > 0 {
				r = rune(glyphs[0])
				for _, g := range glyphs[1:] {
					combining = append(combining, rune(g))
				}
			}
			screen.SetContent(x+col, y+row, r, combining, style)
		}
	}
	visible, _ := t.state.CursorVisible()
	inside, _ := t.state.CursorViewportHasValue()
	if t.HasFocus() {
		screen.HideCursor()
	}
	if t.HasFocus() && visible && inside {
		cx, _ := t.state.CursorViewportX()
		cy, _ := t.state.CursorViewportY()
		if int(cx) < width && int(cy) < height {
			screen.ShowCursor(x+int(cx), y+int(cy))
		}
	}
}

func keyMods(mod tcell.ModMask) ghostty.Mods {
	var m ghostty.Mods
	if mod&tcell.ModShift != 0 {
		m |= ghostty.ModShift
	}
	if mod&tcell.ModCtrl != 0 {
		m |= ghostty.ModCtrl
	}
	if mod&tcell.ModAlt != 0 {
		m |= ghostty.ModAlt
	}
	if mod&tcell.ModMeta != 0 {
		m |= ghostty.ModSuper
	}
	return m
}

var terminalKeys = map[tcell.Key]ghostty.Key{
	tcell.KeyEnter: ghostty.KeyEnter, tcell.KeyTab: ghostty.KeyTab,
	tcell.KeyBacktab: ghostty.KeyTab, tcell.KeyEscape: ghostty.KeyEscape,
	tcell.KeyBackspace: ghostty.KeyBackspace, tcell.KeyBackspace2: ghostty.KeyBackspace,
	tcell.KeyUp: ghostty.KeyArrowUp, tcell.KeyDown: ghostty.KeyArrowDown,
	tcell.KeyLeft: ghostty.KeyArrowLeft, tcell.KeyRight: ghostty.KeyArrowRight,
	tcell.KeyHome: ghostty.KeyHome, tcell.KeyEnd: ghostty.KeyEnd,
	tcell.KeyPgUp: ghostty.KeyPageUp, tcell.KeyPgDn: ghostty.KeyPageDown,
	tcell.KeyInsert: ghostty.KeyInsert, tcell.KeyDelete: ghostty.KeyDelete,
}

func (t *Terminal) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return t.WrapInputHandler(func(ev *tcell.EventKey, _ func(tview.Primitive)) {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.vt == nil || t.ctx.Err() != nil {
			return
		}
		e, err := ghostty.NewKeyEvent()
		if err != nil {
			return
		}
		defer e.Close()
		e.SetAction(ghostty.KeyActionPress)
		mods := keyMods(ev.Modifiers())
		key, known := terminalKeys[ev.Key()]
		switch {
		case known:
			e.SetKey(key)
			if ev.Key() == tcell.KeyBacktab {
				mods |= ghostty.ModShift
			}
		case ev.Key() >= tcell.KeyF1 && ev.Key() <= tcell.KeyF12:
			e.SetKey(ghostty.KeyF1 + ghostty.Key(ev.Key()-tcell.KeyF1))
		case ev.Key() == tcell.KeyRune:
			e.SetUTF8(string(ev.Rune()))
			e.SetUnshiftedCodepoint(ev.Rune())
		case ev.Key() >= 0 && ev.Key() < 32:
			// Classic terminal control bytes must survive unchanged (including Ctrl-C).
			b := []byte{byte(ev.Key())}
			if ev.Modifiers()&tcell.ModAlt != 0 {
				b = append([]byte{27}, b...)
			}
			t.SendInput(b)
			return
		default:
			return
		}
		e.SetMods(mods)
		t.keys.SetOptFromTerminal(t.vt)
		b, err := t.keys.Encode(e)
		if err == nil {
			t.SendInput(b)
		}
	})
}

func (t *Terminal) PasteHandler() func(string, func(tview.Primitive)) {
	return t.WrapPasteHandler(func(text string, _ func(tview.Primitive)) {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.vt == nil {
			return
		}
		if len(text) > maxInputBytes-16 {
			t.cancel()
			return
		}
		bracketed, _ := t.vt.ModeGet(ghostty.ModeBracketedPaste)
		if b, err := ghostty.PasteEncode([]byte(text), bracketed); err == nil {
			t.SendInput(b)
		}
	})
}

func (t *Terminal) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return t.WrapMouseHandler(func(action tview.MouseAction, ev *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		px, py := ev.Position()
		x, y, w, h := t.GetInnerRect()
		if px < x || py < y || px >= x+w || py >= y+h {
			return false, nil
		}
		focus(t)
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.vt == nil {
			return true, nil
		}
		e, err := ghostty.NewMouseEvent()
		if err != nil {
			return true, nil
		}
		defer e.Close()
		e.SetPosition(ghostty.MousePosition{X: float32(px - x), Y: float32(py - y)})
		e.SetMods(keyMods(ev.Modifiers()))
		e.SetAction(ghostty.MouseActionPress)
		switch action {
		case tview.MouseLeftDown:
			e.SetButton(ghostty.MouseButtonLeft)
		case tview.MouseLeftUp:
			e.SetButton(ghostty.MouseButtonLeft)
			e.SetAction(ghostty.MouseActionRelease)
		case tview.MouseMiddleDown:
			e.SetButton(ghostty.MouseButtonMiddle)
		case tview.MouseMiddleUp:
			e.SetButton(ghostty.MouseButtonMiddle)
			e.SetAction(ghostty.MouseActionRelease)
		case tview.MouseRightDown:
			e.SetButton(ghostty.MouseButtonRight)
		case tview.MouseRightUp:
			e.SetButton(ghostty.MouseButtonRight)
			e.SetAction(ghostty.MouseActionRelease)
		case tview.MouseScrollUp:
			e.SetButton(ghostty.MouseButtonFour)
		case tview.MouseScrollDown:
			e.SetButton(ghostty.MouseButtonFive)
		case tview.MouseMove:
			e.SetAction(ghostty.MouseActionMotion)
		default:
			return true, nil
		}
		t.mouse.SetOptFromTerminal(t.vt)
		t.mouse.SetOptSize(ghostty.MouseEncoderSize{ScreenWidth: uint32(w), ScreenHeight: uint32(h), CellWidth: 1, CellHeight: 1})
		t.mouse.SetOptAnyButtonPressed(ev.Buttons()&(tcell.Button1|tcell.Button2|tcell.Button3) != 0)
		b, err := t.mouse.Encode(e)
		if err == nil {
			t.SendInput(b)
		}
		return true, nil
	})
}
