package sshd

type CommandData struct {
	Input     string `json:"input"`
	Output    string `json:"output"`
	Timestamp int64  `json:"timestamp"`
}
