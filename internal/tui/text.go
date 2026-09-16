package tui

import (
	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strings"
)

// TextView uses a Bubbles viewport for scrolling. Existing tagged text remains
// a presentation format, not a dependency on tview's TextView component.
type TextView struct {
	*tview.Box
	model                               viewport.Model
	text                                string
	lines                               []string
	style                               tcell.Style
	dynamic, wrap, wordWrap, scrollable bool
	align, width                        int
	dirty                               bool
	done                                func(tcell.Key)
}

func NewTextView() *TextView {
	m := viewport.New()
	m.SoftWrap = false
	return &TextView{Box: tview.NewBox().SetBackgroundColor(Panel), model: m, style: tcell.StyleDefault.Foreground(Foreground).Background(Panel), wrap: true, wordWrap: true, scrollable: true, dirty: true}
}
func (v *TextView) SetText(s string) *TextView {
	if v.text != s {
		v.text = s
		v.dirty = true
	}
	return v
}
func (v *TextView) GetText(strip bool) string {
	if !strip || !v.dynamic {
		return v.text
	}
	return plainTagged(v.text)
}
func (v *TextView) SetTextStyle(s tcell.Style) *TextView    { v.style = s; return v }
func (v *TextView) SetDynamicColors(b bool) *TextView       { v.dynamic = b; v.dirty = true; return v }
func (v *TextView) SetWrap(b bool) *TextView                { v.wrap = b; v.dirty = true; return v }
func (v *TextView) SetWordWrap(b bool) *TextView            { v.wordWrap = b; v.dirty = true; return v }
func (v *TextView) SetScrollable(b bool) *TextView          { v.scrollable = b; return v }
func (v *TextView) SetTextAlign(a int) *TextView            { v.align = a; return v }
func (v *TextView) SetDoneFunc(f func(tcell.Key)) *TextView { v.done = f; return v }
func (v *TextView) SetSize(rows, cols int) *TextView {
	v.prepare(max(1, cols))
	if rows > 0 {
		v.model.SetHeight(rows)
	}
	return v
}
func (v *TextView) ScrollToBeginning() *TextView { v.model.GotoTop(); v.model.SetXOffset(0); return v }
func (v *TextView) ScrollTo(row, col int) *TextView {
	v.model.SetYOffset(row)
	v.model.SetXOffset(col)
	return v
}
func (v *TextView) GetScrollOffset() (int, int) { return v.model.YOffset(), v.model.XOffset() }
func (v *TextView) GetWrappedLineCount() int {
	_, _, w, _ := v.GetInnerRect()
	v.prepare(max(1, w))
	return len(v.lines)
}
func (v *TextView) prepare(w int) {
	if v.width == w && !v.dirty {
		return
	}
	v.width = w
	v.dirty = false
	text := v.text
	if v.wrap {
		if v.dynamic {
			v.lines = tview.WordWrap(text, w)
		} else {
			if v.wordWrap {
				text = ansi.Wrap(text, w, "")
			} else {
				text = ansi.Hardwrap(text, w, true)
			}
			v.lines = strings.Split(text, "\n")
		}
	} else {
		v.lines = strings.Split(text, "\n")
	}
	// The viewport owns row offsets; tagged labels are painted below so the same
	// escaped data and mnemonic colors survive the component migration.
	v.model.SetWidth(w)
	v.model.SetContentLines(v.lines)
}
func (v *TextView) Draw(s tcell.Screen) {
	v.Box.DrawForSubclass(s, v)
	x, y, w, h := v.GetInnerRect()
	if w < 1 || h < 1 {
		return
	}
	v.prepare(w)
	v.model.SetHeight(h)
	start := v.model.YOffset()
	if !v.scrollable {
		start = 0
	}
	for row := 0; row < h && start+row < len(v.lines); row++ {
		text := v.lines[start+row]
		if v.dynamic {
			DrawStyled(s, lipStyle(v.style).Width(w).Render(""), x, y+row, w, 1)
			fg, _, _ := v.style.Decompose()
			tview.Print(s, text, x, y+row, w, v.align, fg)
		} else {
			if v.scrollable && !v.wrap {
				text = ansi.Cut(text, v.model.XOffset(), v.model.XOffset()+w)
			}
			style := lipStyle(v.style).Width(w).MaxWidth(w)
			if v.align == tview.AlignCenter {
				style = style.Align(0.5)
			} else if v.align == tview.AlignRight {
				style = style.Align(1)
			}
			DrawStyled(s, style.Render(text), x, y+row, w, 1)
		}
	}
}
func (v *TextView) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return v.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		switch e.Key() {
		case tcell.KeyEnter, tcell.KeyEscape, tcell.KeyTab, tcell.KeyBacktab:
			if v.done != nil {
				v.done(e.Key())
			}
			return
		}
		if v.scrollable {
			if e.Key() == tcell.KeyHome || e.Key() == tcell.KeyRune && e.Rune() == 'g' {
				v.model.GotoTop()
				return
			}
			if e.Key() == tcell.KeyEnd || e.Key() == tcell.KeyRune && e.Rune() == 'G' {
				v.model.GotoBottom()
				return
			}
			if msg, ok := bubbleKey(e); ok {
				v.model, _ = v.model.Update(msg)
			}
		}
	})
}
func (v *TextView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return v.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if !v.InRect(e.Position()) {
			return false, nil
		}
		switch a {
		case tview.MouseLeftClick:
			focus(v)
			return true, nil
		case tview.MouseScrollUp:
			if v.scrollable {
				v.model.ScrollUp(3)
				return true, nil
			}
		case tview.MouseScrollDown:
			if v.scrollable {
				v.model.ScrollDown(3)
				return true, nil
			}
		}
		return false, nil
	})
}

// Decode only tview's valid style tags; literal brackets escaped by Escape must
// remain literal, including asset names which resemble color tags.
func plainTagged(text string) string {
	var out strings.Builder
	for len(text) > 0 {
		if text[0] != '[' {
			out.WriteByte(text[0])
			text = text[1:]
			continue
		}
		end := strings.IndexByte(text, ']')
		if end < 0 {
			out.WriteString(text)
			break
		}
		tag := text[1:end]
		if strings.HasSuffix(tag, "[") {
			out.WriteByte('[')
			out.WriteString(strings.TrimSuffix(tag, "["))
			out.WriteByte(']')
			text = text[end+1:]
			continue
		}
		parts := strings.Split(tag, ":")
		valid := len(parts) <= 3 && (parts[0] == "" || parts[0] == "-" || tcell.GetColor(parts[0]) != tcell.ColorDefault)
		if valid && len(parts) > 1 {
			valid = parts[1] == "" || parts[1] == "-" || tcell.GetColor(parts[1]) != tcell.ColorDefault
		}
		if valid && len(parts) > 2 {
			for _, r := range parts[2] {
				if !strings.ContainsRune("-blidrusBLIDRUS", r) {
					valid = false
				}
			}
		}
		if valid {
			text = text[end+1:]
		} else {
			out.WriteByte('[')
			text = text[1:]
		}
	}
	return out.String()
}
