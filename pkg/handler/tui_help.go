package handler

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jumpserver/koko/internal/tui"
)

// Keep the help text itself focused so its native keyboard and wheel scrolling
// still work. The last column is a position indicator for long shortcut lists.
type tuiHelpTextView struct {
	*tui.TextView
}

func (v *tuiHelpTextView) Draw(screen tcell.Screen) {
	_, _, width, _ := v.GetInnerRect()
	v.SetSize(0, width)
	v.TextView.Draw(screen)
	x, y, width, height := v.GetInnerRect()
	if width == 0 || height == 0 {
		return
	}
	total := v.GetWrappedLineCount()
	if total <= height {
		return
	}
	offset, _ := v.GetScrollOffset()
	thumbHeight := max(1, height*height/total)
	thumbTop := min(height-thumbHeight, offset*(height-thumbHeight)/(total-height))
	for row := 0; row < height; row++ {
		ch, color := '│', tui.Border
		if row >= thumbTop && row < thumbTop+thumbHeight {
			ch, color = '█', tui.Accent
		}
		screen.SetContent(x+width+1, y+row, ch, nil, tcell.StyleDefault.Foreground(color).Background(tui.Panel))
	}
}

func (v *tuiHelpTextView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	native := v.TextView.MouseHandler()
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		innerX, innerY, width, height := v.GetInnerRect()
		if action == tview.MouseLeftClick && x == innerX+width+1 && y >= innerY && y < innerY+height {
			if total := v.GetWrappedLineCount(); total > height {
				v.ScrollTo((y-innerY)*(total-height)/max(1, height-1), 0)
			}
			setFocus(v.TextView)
			return true, nil
		}
		return native(action, event, setFocus)
	}
}

func (h *terminalUI) keyboardHelp(commands []tuiShortcut) string {
	var text strings.Builder
	for _, binding := range commands {
		if binding.label == "" {
			continue
		}
		text.WriteString(binding.label + "  " + binding.description + "\n\n")
	}
	return strings.TrimSpace(text.String())
}
