package tui

import (
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Application runs the workspace on Bubble Tea. Existing interactive primitives
// are adapters during the migration; tcell retains sole ownership of the SSH
// transport, including IME cursor positioning and ZMODEM passthrough.
type Application struct {
	screen                       tcell.Screen
	root, focus, mouseCapture    tview.Primitive
	fullscreen, mouse, paste     bool
	before                       func(tcell.Screen) bool
	after                        func(tcell.Screen)
	input                        func(*tcell.EventKey) *tcell.EventKey
	done                         chan struct{}
	stop                         sync.Once
	err                          error
	pasting                      bool
	pasteBuffer                  strings.Builder
	buttons                      tcell.ButtonMask
	mouseX, mouseY, downX, downY int
	clickButton                  tcell.ButtonMask
	lastClick                    time.Time
}

func NewApplication() *Application                           { return &Application{done: make(chan struct{})} }
func (a *Application) SetScreen(s tcell.Screen) *Application { a.screen = s; return a }
func (a *Application) EnableMouse(enabled bool) *Application { a.mouse = enabled; return a }
func (a *Application) EnablePaste(enabled bool) *Application { a.paste = enabled; return a }
func (a *Application) SetInputCapture(f func(*tcell.EventKey) *tcell.EventKey) *Application {
	a.input = f
	return a
}
func (a *Application) SetBeforeDrawFunc(f func(tcell.Screen) bool) *Application {
	a.before = f
	return a
}
func (a *Application) SetAfterDrawFunc(f func(tcell.Screen)) *Application { a.after = f; return a }
func (a *Application) SetRoot(root tview.Primitive, fullscreen bool) *Application {
	a.root, a.fullscreen = root, fullscreen
	return a.SetFocus(root)
}
func (a *Application) SetFocus(p tview.Primitive) *Application {
	if a.focus != nil {
		a.focus.Blur()
	}
	a.focus = p
	if p != nil {
		p.Focus(func(next tview.Primitive) { a.SetFocus(next) })
	}
	return a
}
func (a *Application) GetFocus() tview.Primitive { return a.focus }
func (a *Application) Stop()                     { a.stop.Do(func() { close(a.done) }) }

func (a *Application) Run() error {
	defer a.screen.Fini()
	defer a.Stop()
	if a.mouse {
		a.screen.EnableMouse()
	}
	if a.paste {
		a.screen.EnablePaste()
	}
	// The server must never read stdin, write stdout, or install process-wide
	// signal handlers for an individual SSH login.
	p := tea.NewProgram(a, tea.WithInput(nil), tea.WithOutput(io.Discard),
		tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
	_, err := p.Run()
	if a.err != nil {
		return a.err
	}
	return err
}

type screenEvent struct{ event tcell.Event }

func (a *Application) nextEvent() tea.Msg { return screenEvent{a.screen.PollEvent()} }
func (a *Application) Init() tea.Cmd {
	a.draw()
	return tea.Batch(a.nextEvent, func() tea.Msg { <-a.done; return tea.Quit() })
}

// Rendering goes through the session's color/transport adapter, never a second
// terminal writer. Bubble Tea owns all state transitions and command dispatch.
func (a *Application) View() tea.View { return tea.NewView("") }
func (a *Application) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	event, ok := msg.(screenEvent)
	if !ok {
		return a, nil
	}
	if event.event == nil {
		a.Stop()
		return a, tea.Quit
	}
	redraw := true
	switch ev := event.event.(type) {
	case *tcell.EventKey:
		if a.pasting {
			// Bound memory even when a client sends an unterminated paste.
			if a.pasteBuffer.Len() < 256*1024 {
				switch ev.Key() {
				case tcell.KeyRune:
					a.pasteBuffer.WriteRune(ev.Rune())
				case tcell.KeyEnter:
					a.pasteBuffer.WriteByte('\n')
				case tcell.KeyTab:
					a.pasteBuffer.WriteByte('\t')
				}
			}
			redraw = false
			break
		}
		if a.input != nil {
			ev = a.input(ev)
		}
		if ev != nil && a.root != nil && a.root.HasFocus() {
			if handle := a.root.InputHandler(); handle != nil {
				handle(ev, a.setFocus)
			}
		}
	case *tcell.EventPaste:
		if !a.paste {
			break
		}
		if ev.Start() {
			a.pasting = true
			a.pasteBuffer.Reset()
			redraw = false
		} else if ev.End() {
			a.pasting = false
			if a.root != nil && a.root.HasFocus() && a.pasteBuffer.Len() > 0 {
				if handle := a.root.PasteHandler(); handle != nil {
					handle(a.pasteBuffer.String(), a.setFocus)
				}
			}
			a.pasteBuffer.Reset()
		}
	case *tcell.EventMouse:
		redraw = a.mouse && a.dispatchMouse(ev)
	case *tcell.EventError:
		a.err = ev
		a.Stop()
		return a, tea.Quit
	case *tcell.EventResize:
	default:
		redraw = false
	}
	select {
	case <-a.done:
		return a, tea.Quit
	default:
	}
	if redraw {
		a.draw()
	}
	return a, a.nextEvent
}

func (a *Application) setFocus(p tview.Primitive) { a.SetFocus(p) }
func (a *Application) draw() {
	if a.root == nil {
		return
	}
	if a.fullscreen {
		w, h := a.screen.Size()
		a.root.SetRect(0, 0, w, h)
	}
	a.screen.Clear()
	if a.before == nil || !a.before(a.screen) {
		a.root.Draw(a.screen)
		if a.after != nil {
			a.after(a.screen)
		}
	}
	a.screen.Show()
}

func (a *Application) dispatchMouse(ev *tcell.EventMouse) bool {
	consumed := false
	target := a.mouseCapture
	fire := func(action tview.MouseAction) {
		p := target
		if p == nil {
			p = a.root
		}
		if p != nil {
			if handle := p.MouseHandler(); handle != nil {
				used, capture := handle(action, ev, a.setFocus)
				consumed = consumed || used
				a.mouseCapture = capture
				if capture != nil {
					target = capture
				}
			}
		}
	}
	x, y := ev.Position()
	buttons := ev.Buttons()
	if x != a.mouseX || y != a.mouseY {
		fire(tview.MouseMove)
	}
	for _, b := range []struct {
		mask                    tcell.ButtonMask
		down, up, click, double tview.MouseAction
	}{
		{tcell.ButtonPrimary, tview.MouseLeftDown, tview.MouseLeftUp, tview.MouseLeftClick, tview.MouseLeftDoubleClick},
		{tcell.ButtonMiddle, tview.MouseMiddleDown, tview.MouseMiddleUp, tview.MouseMiddleClick, tview.MouseMiddleDoubleClick},
		{tcell.ButtonSecondary, tview.MouseRightDown, tview.MouseRightUp, tview.MouseRightClick, tview.MouseRightDoubleClick},
	} {
		if (buttons^a.buttons)&b.mask == 0 {
			continue
		}
		if buttons&b.mask != 0 {
			if x != a.downX || y != a.downY {
				a.lastClick = time.Time{}
			}
			a.downX, a.downY = x, y
			fire(b.down)
		} else {
			fire(b.up)
			if x == a.downX && y == a.downY {
				if a.clickButton == b.mask && time.Since(a.lastClick) < tview.DoubleClickInterval {
					fire(b.double)
					a.lastClick = time.Time{}
				} else {
					fire(b.click)
					a.lastClick, a.clickButton = time.Now(), b.mask
				}
			}
		}
	}
	for _, wheel := range []struct {
		mask   tcell.ButtonMask
		action tview.MouseAction
	}{
		{tcell.WheelUp, tview.MouseScrollUp}, {tcell.WheelDown, tview.MouseScrollDown},
		{tcell.WheelLeft, tview.MouseScrollLeft}, {tcell.WheelRight, tview.MouseScrollRight},
	} {
		if buttons&wheel.mask != 0 {
			fire(wheel.action)
		}
	}
	a.buttons, a.mouseX, a.mouseY = buttons, x, y
	return consumed
}
