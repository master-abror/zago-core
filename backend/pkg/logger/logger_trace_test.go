package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func TestTraceIDIsLoggedOnlyWhenPresent(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)

	log.InfoContext(context.Background(), "tanpa trace")
	ctx := logger.WithTraceID(logger.WithRequestID(context.Background(), "req-1"), "trace-abc")
	log.InfoContext(ctx, "dengan trace")

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 2)
	_, has := lines[0]["trace_id"]
	require.False(t, has, "trace_id tidak muncul bila tak ada pada context")
	require.Equal(t, "trace-abc", lines[1]["trace_id"])
	require.Equal(t, "req-1", lines[1]["request_id"])
	require.Equal(t, "trace-abc", logger.TraceID(ctx))
	require.Empty(t, logger.TraceID(context.Background()))
}
