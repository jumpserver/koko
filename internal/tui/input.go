package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

// InputField delegates editing to Bubbles. Box is only the temporary layout and
// focus adapter; a real SSH cursor anchors client-side IME composition.
type InputField struct {
	*tview.Box
	model                                    textinput.Model
	label                                    string
	offset                                   int
	fieldStyle, labelStyle, placeholderStyle tcell.Style
	changed                                  func(string)
	done                                     func(tcell.Key)
}

func NewInputField() *InputField {
	m := textinput.New()
	m.Prompt = ""
	m.SetVirtualCursor(false)
	// SSH clipboard data arrives as bracketed paste, never from the server OS.
	m.KeyMap.Paste.SetEnabled(false)
	return &InputField{Box: tview.NewBox().SetBackgroundColor(Panel), model: m, fieldStyle: tcell.StyleDefault.Foreground(Foreground).Background(Panel), labelStyle: tcell.StyleDefault.Foreground(Accent).Background(Panel), placeholderStyle: tcell.StyleDefault.Foreground(Muted).Background(Panel)}
}
func (i *InputField) SetText(s string) *InputField {
	old := i.model.Value()
	i.model.SetValue(s)
	i.model.CursorEnd()
	i.offset = 0
	if old != i.model.Value() && i.changed != nil {
		i.changed(i.model.Value())
	}
	return i
}
func (i *InputField) GetText() string                         { return i.model.Value() }
func (i *InputField) SetLabel(s string) *InputField           { i.label = s; return i }
func (i *InputField) GetLabel() string                        { return i.label }
func (i *InputField) SetPlaceholder(s string) *InputField     { i.model.Placeholder = s; return i }
func (i *InputField) SetMaxLength(n int) *InputField          { i.model.CharLimit = n; return i }
func (i *InputField) SetFieldStyle(s tcell.Style) *InputField { i.fieldStyle = s; return i }
func (i *InputField) SetFieldTextColor(c tcell.Color) *InputField {
	i.fieldStyle = i.fieldStyle.Foreground(c)
	return i
}
func (i *InputField) SetFieldBackgroundColor(c tcell.Color) *InputField {
	i.fieldStyle = i.fieldStyle.Background(c)
	return i
}
func (i *InputField) SetLabelStyle(s tcell.Style) *InputField       { i.labelStyle = s; return i }
func (i *InputField) SetPlaceholderStyle(s tcell.Style) *InputField { i.placeholderStyle = s; return i }
func (i *InputField) SetChangedFunc(f func(string)) *InputField     { i.changed = f; return i }
func (i *InputField) SetDoneFunc(f func(tcell.Key)) *InputField     { i.done = f; return i }
func (i *InputField) Focus(f func(tview.Primitive))                 { i.Box.Focus(f); i.model.Focus() }
func (i *InputField) Blur()                                         { i.Box.Blur(); i.model.Blur() }

func lipStyle(s tcell.Style) lipgloss.Style {
	fg, bg, attrs := s.Decompose()
	out := lipgloss.NewStyle().Bold(attrs&tcell.AttrBold != 0).Underline(attrs&tcell.AttrUnderline != 0).Italic(attrs&tcell.AttrItalic != 0).Faint(attrs&tcell.AttrDim != 0)
	if fg != tcell.ColorDefault {
		out = out.Foreground(StyleColor(fg))
	}
	if bg != tcell.ColorDefault {
		out = out.Background(StyleColor(bg))
	}
	return out
}
func (i *InputField) Draw(s tcell.Screen) {
	i.Box.DrawForSubclass(s, i)
	x, y, w, h := i.GetInnerRect()
	if w < 1 || h < 1 {
		return
	}
	fg, _, _ := i.labelStyle.Decompose()
	_, used := tview.Print(s, i.label, x, y, w, tview.AlignLeft, fg)
	x += used
	w -= used
	if w < 1 {
		return
	}
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = lipStyle(i.fieldStyle)
	styles.Focused.Placeholder = lipStyle(i.placeholderStyle)
	styles.Blurred = styles.Focused
	i.model.SetStyles(styles)
	// Bubbles' real cursor position is rune-based. Keep the viewport in terminal
	// cells so Chinese and combining characters retain the correct IME anchor.
	value := []rune(i.model.Value())
	pos := runewidth.StringWidth(string(value[:i.model.Position()]))
	i.offset = max(0, min(i.offset, pos))
	if pos-i.offset >= w {
		i.offset = pos - w + 1
	}
	i.model.SetWidth(0)
	view := i.model.View()
	if i.model.Value() == "" {
		view = lipStyle(i.placeholderStyle).Render(i.model.Placeholder)
	}
	view = ansi.Cut(view, i.offset, i.offset+w)
	DrawStyled(s, lipStyle(i.fieldStyle).Width(w).Render(view), x, y, w, 1)
	if i.HasFocus() {
		s.ShowCursor(x+min(w-1, pos-i.offset), y)
		s.SetCursorStyle(tcell.CursorStyleBlinkingBar)
	}
}
func (i *InputField) update(msg tea.Msg) {
	old := i.model.Value()
	focused := i.model.Focused()
	if !focused {
		i.model.Focus()
	}
	i.model, _ = i.model.Update(msg)
	if !focused {
		i.model.Blur()
	}
	if old != i.model.Value() && i.changed != nil {
		i.changed(i.model.Value())
	}
}
func (i *InputField) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return i.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		switch e.Key() {
		case tcell.KeyEnter, tcell.KeyEscape, tcell.KeyTab, tcell.KeyBacktab:
			if i.done != nil {
				i.done(e.Key())
			}
			return
		}
		if msg, ok := bubbleKey(e); ok {
			i.update(msg)
		}
	})
}
func (i *InputField) PasteHandler() func(string, func(tview.Primitive)) {
	return i.WrapPasteHandler(func(s string, _ func(tview.Primitive)) {
		i.update(tea.PasteMsg{Content: strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ")})
	})
}
func (i *InputField) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return i.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if !i.InRect(e.Position()) {
			return false, nil
		}
		if a != tview.MouseLeftDown && a != tview.MouseLeftClick {
			return false, nil
		}
		focus(i)
		x, _, _, _ := i.GetInnerRect()
		mx, _ := e.Position()
		target := max(0, mx-x-tview.TaggedStringWidth(i.label)+i.offset)
		col, index := 0, 0
		for n, r := range []rune(i.model.Value()) {
			if col+runewidth.RuneWidth(r) > target {
				break
			}
			col += runewidth.RuneWidth(r)
			index = n + 1
		}
		i.model.SetCursor(index)
		return true, nil
	})
}

func bubbleKey(e *tcell.EventKey) (tea.KeyPressMsg, bool) {
	k := tea.Key{Code: e.Rune()}
	if e.Modifiers()&tcell.ModAlt != 0 {
		k.Mod |= tea.ModAlt
	}
	if e.Modifiers()&tcell.ModCtrl != 0 {
		k.Mod |= tea.ModCtrl
	}
	if e.Modifiers()&tcell.ModShift != 0 {
		k.Mod |= tea.ModShift
	}
	if e.Key() == tcell.KeyRune {
		if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			k.Text = string(e.Rune())
		}
		return tea.KeyPressMsg(k), true
	}
	codes := map[tcell.Key]rune{tcell.KeyLeft: tea.KeyLeft, tcell.KeyRight: tea.KeyRight, tcell.KeyUp: tea.KeyUp, tcell.KeyDown: tea.KeyDown, tcell.KeyHome: tea.KeyHome, tcell.KeyEnd: tea.KeyEnd, tcell.KeyPgUp: tea.KeyPgUp, tcell.KeyPgDn: tea.KeyPgDown, tcell.KeyBackspace: tea.KeyBackspace, tcell.KeyBackspace2: tea.KeyBackspace, tcell.KeyDelete: tea.KeyDelete, tcell.KeyEnter: tea.KeyEnter, tcell.KeyEscape: tea.KeyEscape, tcell.KeyTab: tea.KeyTab, tcell.KeyBacktab: tea.KeyTab}
	if code, ok := codes[e.Key()]; ok {
		k.Code = code
		if e.Key() == tcell.KeyBacktab {
			k.Mod |= tea.ModShift
		}
		return tea.KeyPressMsg(k), true
	}
	if e.Key() >= tcell.KeyCtrlA && e.Key() <= tcell.KeyCtrlZ {
		k.Code = 'a' + rune(e.Key()-tcell.KeyCtrlA)
		k.Mod |= tea.ModCtrl
		return tea.KeyPressMsg(k), true
	}
	return tea.KeyPressMsg{}, false
}
