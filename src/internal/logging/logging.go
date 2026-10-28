// Package logging builds the slog logger every binary starts with.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// New reads LOG_FORMAT (json or text, default json) and LOG_LEVEL (debug,
// info, warn or error, default info), tags every record with service, and
// makes the result the default, so slog.Default and the log package use it.
func New(service string) *slog.Logger {
	return setup(os.Stderr, service, os.Getenv("LOG_FORMAT"), os.Getenv("LOG_LEVEL"))
}

func setup(w io.Writer, service, format, level string) *slog.Logger {
	h, err := newHandler(w, format, level)
	logger := slog.New(h).With("service", service)
	slog.SetDefault(logger)
	if err != nil {
		logger.Warn("logging config", "err", err)
	}
	return logger
}

// newHandler falls back to the defaults on a bad value, so a typo in the env
// never stops a server from starting.
func newHandler(w io.Writer, format, level string) (slog.Handler, error) {
	var errs []string
	lvl := slog.LevelInfo
	if level != "" {
		if err := lvl.UnmarshalText([]byte(level)); err != nil {
			lvl = slog.LevelInfo
			errs = append(errs, fmt.Sprintf("LOG_LEVEL %q is not debug, info, warn or error", level))
		}
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	switch strings.ToLower(format) {
	case "", "json":
		h = slog.NewJSONHandler(w, opts)
	case "text":
		h = slog.NewTextHandler(w, opts)
	default:
		h = slog.NewJSONHandler(w, opts)
		errs = append(errs, fmt.Sprintf("LOG_FORMAT %q is not json or text", format))
	}
	if len(errs) > 0 {
		return h, fmt.Errorf("%s, using the default", strings.Join(errs, "; "))
	}
	return h, nil
}
