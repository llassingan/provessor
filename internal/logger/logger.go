package logger

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Logger struct {
	dev    bool
	slog   *slog.Logger
	file   *os.File
}

func New(dev bool, logPath string) (*Logger, error) {
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	var f *os.File
	if dev && logPath != "" {
		dir := filepath.Dir(logPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, err
		}
		var err error
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, f)
	}

	w := io.MultiWriter(writers...)
	log.SetOutput(w)
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return &Logger{
		dev:  dev,
		slog: slog.New(handler),
		file: f,
	}, nil
}

func (l *Logger) Close() error {
	log.SetOutput(os.Stderr)
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *Logger) Debug(msg string, args ...any) {
	if !l.dev {
		return
	}
	l.slog.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.slog.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.slog.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.slog.Error(msg, args...)
}

// Nop returns a Logger that discards all output. Safe for tests.
func Nop() *Logger {
	return &Logger{
		dev:  false,
		slog: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// ── Redaction helpers ─────────────────────────────────────────

// Redact returns a masked representation of sensitive values.
// For non-empty strings, returns "[redacted]". For empty strings, returns "<empty>".
// When v is a string and len(v) > maxPlain, only the first maxPlain characters are
// shown followed by "...".
func Redact(v any) any {
	switch s := v.(type) {
	case string:
		if s == "" {
			return "<empty>"
		}
		return "[redacted]"
	default:
		return v
	}
}

// sensitiveKeys lists field names whose values must never appear in logs.
var sensitiveKeys = map[string]bool{
	"password":       true,
	"passwd":         true,
	"new_password":   true,
	"ssh_password":   true,
	"private_key":    true,
	"secret":         true,
	"token":          true,
	"api_token":      true,
	"authorization":  true,
	"credentials":    true,
	"body":           true, // request/response bodies
	"payload":        true,
	"callback_token": true,
	"ssh_private_key": true,
}

// CleanArgs returns a copy of key=value pairs with sensitive values replaced by "[redacted]".
// Each element in args should alternate: key, value, key, value.
func CleanArgs(args ...any) []any {
	if len(args) == 0 {
		return args
	}
	out := make([]any, len(args))
	copy(out, args)
	for i := 0; i+1 < len(out); i += 2 {
		key, ok := out[i].(string)
		if !ok {
			continue
		}
		if sensitiveKeys[strings.ToLower(key)] {
			out[i+1] = "[redacted]"
		}
	}
	return out
}
