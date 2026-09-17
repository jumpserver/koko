package guacd

// Network parameters
const (
	RDPHostname = "hostname"
	RDPPort     = "port" // default 3389
)

// Authentication and security

const (
	RDPUsername    = "username"
	RDPPassword    = "password"
	RDPDomain      = "domain"
	RDPSecurity    = "security" // any | nla | nla-ext | tls | vmconnect | rdp
	RDPIgnoreCert  = "ignore-cert"
	RDPDisableAuth = "disable-auth"
)

// Session settings

const (
	RDPClientName     = "client-name"
	RDPConsole        = "console"
	RDPInitialProgram = "initial-program"
	RDPServerLayout   = "server-layout"
	RDPTimezone       = "timezone"
)

// Display settings

const (
	RDPColorDepth   = "color-depth"
	RDPWidth        = "width"
	RDPHeight       = "height"
	RDPDpi          = "dpi"
	RDPResizeMethod = "resize-method" // display-update| reconnect
)

// Device redirection
// https://tools.ietf.org/html/rfc4856
const (
	RDPDisableAudio     = "disable-audio"
	RDPEnableAudioInput = "enable-audio-input"
	RDPPrinterName      = "printer-name"
	RDPEnableDrive      = "enable-drive"
	RDPDisableDownload  = "disable-download"
	RDPDisableUpload    = "disable-upload"
	RDPDriveName        = "drive-name"
	RDPDrivePath        = "drive-path"
	RDPCreateDrivePath  = "create-drive-path"
	RDPConsoleAudio     = "console-audio"
	RDPStaticChannels   = "static-channels"
)

// Preconnection PDU (Hyper-V / VMConnect)

const (
	RDPPreConnectionId   = "preconnection-id"
	RDPPreConnectionBlob = "preconnection-blob"
)

// Remote desktop gateway

const (
	RDPGatewayHostname = "gateway-hostname"
	RDPGatewayPort     = "gateway-port"
	RDPGatewayUsername = "gateway-username"
	RDPGatewayPassword = "gateway-password"
	RDPGatewayDomain   = "gateway-domain"
)

// Load balancing and RDP connection brokers

const (
	RDPLoadBalanceInfo = "load-balance-info"
)

// Performance flags

const (
	RDPEnableWallpaper          = "enable-wallpaper"
	RDPEnableTheming            = "enable-theming"
	RDPEnableFontSmoothing      = "enable-font-smoothing"
	RDPEnableFullWindowDrag     = "enable-full-window-drag"
	RDPEnableDesktopComposition = "enable-desktop-composition"
	RDPEnableMenuAnimations     = "enable-menu-animations"
	RDPDisableBitmapCaching     = "disable-bitmap-caching"
	RDPDisableOffscreenCaching  = "disable-offscreen-caching"
	RDPDisableGlyphCaching      = "disable-glyph-caching"
)

// RemoteApp

const (
	RDPRemoteApp     = "remote-app"
	RDPRemoteAppDir  = "remote-app-dir"
	RDPRemoteAppArgs = "remote-app-args"
)
