package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	for _, tc := range []struct {
		format  string
		json    bool
		wantErr bool
	}{
		{"", true, false},
		{"json", true, false},
		{"JSON", true, false},
		{"text", false, false},
		{"yaml", true, true},
	} {
		var buf bytes.Buffer
		h, err := newHandler(&buf, tc.format, "")
		if (err != nil) != tc.wantErr {
			t.Errorf("format %q: err = %v, want error %v", tc.format, err, tc.wantErr)
		}
		slog.New(h).Info("hello", "k", "v")
		var rec map[string]any
		isJSON := json.Unmarshal(buf.Bytes(), &rec) == nil
		if isJSON != tc.json {
			t.Errorf("format %q: wrote %q, want json %v", tc.format, buf.String(), tc.json)
		}
	}
}

func TestLevel(t *testing.T) {
	for _, tc := range []struct {
		level   string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"WARN", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"loud", slog.LevelInfo, true},
	} {
		h, err := newHandler(&bytes.Buffer{}, "", tc.level)
		if (err != nil) != tc.wantErr {
			t.Errorf("level %q: err = %v, want error %v", tc.level, err, tc.wantErr)
		}
		if !h.Enabled(context.Background(), tc.want) || (tc.want > slog.LevelDebug && h.Enabled(context.Background(), tc.want-1)) {
			t.Errorf("level %q: handler does not stop at %v", tc.level, tc.want)
		}
	}
}

func TestSetupSetsDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "test-service", "json", "info")

	slog.Info("from slog", "k", "v")
	log.Print("from log")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %q, want two lines", buf.String())
	}
	for i, want := range []string{"from slog", "from log"} {
		var rec map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &rec); err != nil {
			t.Fatalf("line %q is not JSON", lines[i])
		}
		if rec["msg"] != want || rec["service"] != "test-service" || rec["level"] != "INFO" {
			t.Errorf("record = %v, want msg %q from test-service", rec, want)
		}
	}
}

func TestSetupWarnsOnBadConfig(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "test-service", "yaml", "loud")
	if !strings.Contains(buf.String(), `"msg":"logging config"`) {
		t.Errorf("wrote %q, want a logging config warning", buf.String())
	}
}
