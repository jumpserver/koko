package guacd

var keyidentifierKeysym = map[int]string{
	0xFF3D: "AllCandidates",
	0xFF30: "Alphanumeric",
	0xFFE9: "Alt",
	0xFE03: "Alt",
	0xFD0E: "Attn",
	0xFF54: "ArrowDown",
	0xFF51: "ArrowLeft",
	0xFF53: "ArrowRight",
	0xFF52: "ArrowUp",
	0xFF08: "Backspace",
	0xFFE5: "CapsLock",
	0xFF69: "Cancel",
	0xFF0B: "Clear",
	0xFF21: "Convert",
	0xFD15: "Copy",
	0xFD1C: "Crsel",
	0xFF37: "CodeInput",
	0xFF20: "Compose",
	0xFFE3: "Control",
	0xFFE4: "Control",
	0xFF67: "ContextMenu",
	0xFFFF: "Delete",
	0xFF57: "End",
	0xFF0D: "Enter\r",
	0xFF8D: "Enter\r", // KP_Enter
	0xFD06: "EraseEof",
	0xFF1B: "Escape",
	0xFF62: "Execute",
	0xFD1D: "Exsel",
	0xFFBE: "F1",
	0xFFBF: "F2",
	0xFFC0: "F3",
	0xFFC1: "F4",
	0xFFC2: "F5",
	0xFFC3: "F6",
	0xFFC4: "F7",
	0xFFC5: "F8",
	0xFFC6: "F9",
	0xFFC7: "F10",
	0xFFC8: "F11",
	0xFFC9: "F12",
	0xFFCA: "F13",
	0xFFCB: "F14",
	0xFFCC: "F15",
	0xFFCD: "F16",
	0xFFCE: "F17",
	0xFFCF: "F18",
	0xFFD0: "F19",
	0xFFD1: "F20",
	0xFFD2: "F21",
	0xFFD3: "F22",
	0xFFD4: "F23",
	0xFFD5: "F24",
	0xFF68: "Find",
	0xFE0C: "GroupFirst",
	0xFE0E: "GroupLast",
	0xFE08: "GroupNext",
	0xFE0A: "GroupPrevious",
	0xFF31: "HangulMode",
	0xFF29: "Hankaku",
	0xFF34: "HanjaMode",
	0xFF6A: "Help",
	0xFF25: "Hiragana",
	0xFF27: "HiraganaKatakana",
	0xFF50: "Home",
	0xFFED: "Hyper",
	0xFFEE: "Hyper",
	0xFF63: "Insert",
	0xFF24: "JapaneseRomaji",
	0xFF38: "JunjaMode",
	0xFF2D: "KanaMode",
	0xFF26: "Katakana",
	0xFFE7: "Meta",
	0xFFE8: "Meta",
	0xFF7E: "ModeChange",
	0xFF7F: "NumLock",
	0xFF56: "PageDown",
	0xFF55: "PageUp",
	0xFF13: "Pause",
	0xFD16: "Play",
	0xFF3E: "PreviousCandidate",
	0xFF61: "PrintScreen",
	0xFF66: "Redo",
	0xFF14: "Scroll",
	0xFF60: "Select",
	0xFFAA: "*",         // KP_Multiply
	0xFFAB: "+",         // KP_Add
	0xFFAC: "Separator", // KP_Separator is locale-dependent
	0xFFAD: "-",         // KP_Subtract
	0xFFAE: ".",         // KP_Decimal
	0xFFAF: "/",         // KP_Divide
	0xFFB0: "0",         // KP_0
	0xFFB1: "1",         // KP_1
	0xFFB2: "2",         // KP_2
	0xFFB3: "3",         // KP_3
	0xFFB4: "4",         // KP_4
	0xFFB5: "5",         // KP_5
	0xFFB6: "6",         // KP_6
	0xFFB7: "7",         // KP_7
	0xFFB8: "8",         // KP_8
	0xFFB9: "9",         // KP_9
	0xFFBD: "=",         // KP_Equal
	0xFFE1: "Shift",
	0xFFE2: "Shift",
	0xFF3C: "SingleCandidate",
	0xFFEC: "Super",
	0xFF09: "Tab",
	0xFF65: "Undo",
	0xFFEB: "Win",
	0xFF28: "Zenkaku",
	0xFF2A: "ZenkakuHankaku",
}

const (
	KeyPress   = "1"
	KeyRelease = "0"
)

const (
	MouseLeft   = "1"
	MouseMiddle = "2"
	MouseRight  = "4"
	MouseUp     = "8"
	MouseDown   = "16"
)

const (
	KeyCodeUnknown = "Unknown"
)

func KeysymToCharacter(keysym int) string {
	return keyidentifierKeysym[keysym]
}
