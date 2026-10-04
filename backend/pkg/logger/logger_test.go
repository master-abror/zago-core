package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "baris log harus JSON valid: %s", line)
		out = append(out, m)
	}
	return out
}

func TestLogsAreJSONWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)

	ctx := logger.WithRequestID(context.Background(), "req-123")
	log.InfoContext(ctx, "halo", "k", "v")

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 1)
	require.Equal(t, "halo", lines[0]["msg"])
	require.Equal(t, "INFO", lines[0]["level"])
	require.Equal(t, "req-123", lines[0]["request_id"])
	require.Equal(t, "v", lines[0]["k"])
}

func TestNoRequestIDAttributeWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)

	log.InfoContext(context.Background(), "tanpa id")
	log.Info("tanpa context")

	for _, line := range decodeLines(t, &buf) {
		require.NotContains(t, line, "request_id")
	}
}

func TestRequestIDSurvivesWithAndWithGroup(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo).With("component", "http")

	ctx := logger.WithRequestID(context.Background(), "req-xyz")
	log.InfoContext(ctx, "dari logger turunan")

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 1)
	require.Equal(t, "req-xyz", lines[0]["request_id"])
	require.Equal(t, "http", lines[0]["component"])
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelWarn)

	log.Info("disaring")
	log.Warn("lolos")

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 1)
	require.Equal(t, "lolos", lines[0]["msg"])
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "INFO": slog.LevelInfo, " warn ": slog.LevelWarn,
		"warning": slog.LevelWarn, "Error": slog.LevelError,
	}
	for in, want := range cases {
		got, err := logger.ParseLevel(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "verbose", "trace", "10"} {
		_, err := logger.ParseLevel(bad)
		require.Error(t, err, bad)
	}
}

func TestRequestIDHelpers(t *testing.T) {
	require.Equal(t, "", logger.RequestID(context.Background()))
	ctx := logger.WithRequestID(context.Background(), "abc")
	require.Equal(t, "abc", logger.RequestID(ctx))
}
