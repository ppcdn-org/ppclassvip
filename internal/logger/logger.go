package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ppclassvip/internal/config"
)

func New(cfg config.LogConfig) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	writer := logWriter(cfg.File)
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "json") {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}
	return slog.New(handler)
}

// logWriter always writes to stdout. When a file path is configured, logs are
// also appended to that file so deployments get persistent logs without relying
// on shell redirection.
func logWriter(path string) io.Writer {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.Stdout
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create log directory %s: %v\n", dir, err)
			return os.Stdout
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open log file %s: %v\n", path, err)
		return os.Stdout
	}
	return io.MultiWriter(os.Stdout, file)
}
