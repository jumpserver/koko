package guacd

// Network parameters

const (
	Hostname = "hostname"
	Port     = "port"

	Username = "username"
	Password = "password"
)

// Session Recording
const (
	RecordingPath          = "recording-path"
	CreateRecordingPath    = "create-recording-path"
	RecordingName          = "recording-name"
	RecordingExcludeOutput = "recording-exclude-output"
	RecordingExcludeMouse  = "recording-exclude-mouse"
	RecordingIncludeKeys   = "recording-include-keys"
)

// SFTP

const (
	EnableSftp              = "enable-sftp"
	SftpHostname            = "sftp-hostname"
	SftpPort                = "sftp-port"
	SftpHostKey             = "sftp-host-key"
	SftpUsername            = "sftp-username"
	SftpPassword            = "sftp-password"
	SftpPrivateKey          = "sftp-private-key"
	SftpPassphrase          = "sftp-passphrase"
	SftpDirectory           = "sftp-directory"
	SftpRootDirectory       = "sftp-root-directory"
	SftpServerAliveInterval = "sftp-server-alive-interval"
	SftpDisableDownload     = "sftp-disable-download"
	SftpDisableUpload       = "sftp-disable-upload"
)

// Disabling clipboard access

const (
	DisableCopy  = "disable-copy"
	DisablePaste = "disable-paste"
)

// Wake-on-LAN Configuration

const (
	WolSendPacket    = "wol-send-packet"
	WolMacAddr       = "wol-mac-addr"
	WolBroadcastAddr = "wol-broadcast-addr"
	WolWaitTime      = "wol-wait-time"
)

const (
	READONLY = "read-only"
)

const (
	BoolFalse = "false"
	BoolTrue  = "true"
)
