package tui

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gdamore/tcell/v2"
)

func TestApplicationInputAndShutdown(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 24)
	field := NewInputField()
	app := NewApplication().SetScreen(screen).EnablePaste(true).SetRoot(field, true)
	var value string
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			value = field.GetText()
			app.Stop()
			return nil
		}
		return ev
	})
	done := make(chan error, 1)
	go func() { done <- app.Run() }()
	for _, event := range []tcell.Event{
		tcell.NewEventKey(tcell.KeyRune, 'A', 0),
		tcell.NewEventPaste(true),
		tcell.NewEventKey(tcell.KeyRune, '中', 0),
		tcell.NewEventKey(tcell.KeyRune, '文', 0),
		tcell.NewEventPaste(false),
		tcell.NewEventKey(tcell.KeyEscape, 0, 0),
	} {
		if err := screen.PostEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if value != "A中文" {
			t.Fatalf("input = %q", value)
		}
	case <-time.After(3 * time.Second):
		app.Stop()
		t.Fatal("Bubble Tea did not stop")
	}
}

func TestApplicationMouse(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	button := NewButton("Connect")
	button.SetRect(2, 2, 12, 1)
	clicked := 0
	button.SetSelectedFunc(func() { clicked++ })
	app := NewApplication().SetScreen(screen).SetRoot(button, false)
	app.dispatchMouse(tcell.NewEventMouse(4, 2, tcell.ButtonPrimary, 0))
	app.dispatchMouse(tcell.NewEventMouse(4, 2, tcell.ButtonNone, 0))
	if clicked != 1 {
		t.Fatalf("click dispatched %d times", clicked)
	}
}

func TestStyledCellsFollowPalette(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(20, 3)
	themed := NewThemeScreen(screen)
	palette := ThemePalette(false, 1)
	themed.SetPalette(palette)
	text := lipgloss.NewStyle().Foreground(StyleColor(Accent)).Bold(true).Render("中文 A")
	DrawStyled(themed, text, 1, 1, 12, 1)
	r, _, style, width := screen.GetContent(1, 1)
	fg, _, attrs := style.Decompose()
	if r != '中' || width != 2 || fg != palette.Accent || attrs&tcell.AttrBold == 0 {
		t.Fatalf("styled wide cell = %q, %d, %v", r, width, style)
	}
	if r, _, _, _ = screen.GetContent(6, 1); r != 'A' {
		t.Fatalf("wide-cell alignment = %q", r)
	}
}
