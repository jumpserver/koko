package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const logFileName = "kael.log"

type writeOnly struct {
	io.Writer
}

var levels = map[string]zapcore.Level{
	"DEBUG": zapcore.DebugLevel,
	"INFO":  zapcore.InfoLevel,
	"WARN":  zapcore.WarnLevel,
	"ERROR": zapcore.ErrorLevel,
}

// New creates a logger that writes each entry to stdout and a rotating file.
func New(logDir, levelName string) (*zap.Logger, error) {
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", logDir, err)
	}

	level, ok := levels[strings.ToUpper(strings.TrimSpace(levelName))]
	if !ok {
		level = zapcore.InfoLevel
	}
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:       "time",
		LevelKey:      "level",
		MessageKey:    "message",
		StacktraceKey: "stacktrace",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel: func(value zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
			encoder.AppendString("[" + value.CapitalString() + "]")
		},
		EncodeTime: func(value time.Time, encoder zapcore.PrimitiveArrayEncoder) {
			encoder.AppendString(value.Local().Format("2006-01-02 15:04:05"))
		},
		EncodeDuration:   zapcore.StringDurationEncoder,
		ConsoleSeparator: " ",
	}
	rotatingFile := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, logFileName),
		MaxSize:    50,
		MaxBackups: 7,
		MaxAge:     7,
		LocalTime:  true,
	}
	output := zapcore.NewMultiWriteSyncer(
		zapcore.Lock(zapcore.AddSync(writeOnly{Writer: os.Stdout})),
		zapcore.AddSync(rotatingFile),
	)
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), output, level)
	return zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
