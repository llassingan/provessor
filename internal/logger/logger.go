package logger

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
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
