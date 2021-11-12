package asciinema

import "time"

type Config struct {
	Title     string
	EnvShell  string
	EnvTerm   string
	Width     int
	Height    int
	Timestamp time.Time
}

type Option func(options *Config)

func WithWidth(width int) Option {
	return func(options *Config) {
		options.Width = width
	}
}

func WithHeight(height int) Option {
	return func(options *Config) {
		options.Height = height
	}
}

func WithTimestamp(timestamp time.Time) Option {
	return func(options *Config) {
		options.Timestamp = timestamp
	}
}

func WithTitle(title string) Option {
	return func(options *Config) {
		options.Title = title
	}
}

func WithEnvShell(shell string) Option {
	return func(options *Config) {
		options.EnvShell = shell
	}
}

func WithEnvTerm(term string) Option {
	return func(options *Config) {
		options.EnvTerm = term
	}
}
