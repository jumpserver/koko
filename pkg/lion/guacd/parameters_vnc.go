package guacd

// Network parameters
const (
	VNCHostname  = "hostname"
	VNCPort      = "port"
	VNCAutoretry = "autoretry"
)

// Authentication
const (
	VNCUsername = "username"
	VNCPassword = "password"
)

// Display Settings
const (
	VNCColorDepth  = "color-depth"
	VNCSwapRedBlue = "swap-red-blue"
	VNCCursor      = "cursor"
	VNCEncoding    = "encodings"
	VNCReadOnly    = "read-only"
)

// VNC Repeater

const (
	VNCDestHost = "dest-host"
	VNCDestPort = "dest-port"
)

// Reverse VNC connections

const (
	VNCReverseConnect = "reverse-connect"
	VNCListenTimeout  = "listen-timeout"
)

// Audio support (via PulseAudio)

const (
	VNCEnableAudio     = "enableAudio"
	VNCAudioServername = "audio-servername"
)

// Clipboard encoding

const (
	VNCClipboardEncoding = "clipboard-encoding" // ISO8859-1| UTF-8 | UTF-16| CP1252
)
