package testpg

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigureReaperRaisesRyukConnectionTimeoutWhenUnset(t *testing.T) {
	t.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "")
	t.Setenv("RYUK_CONNECTION_TIMEOUT", "")

	ConfigureReaper()

	require.Equal(t, "3m", os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"),
		"bawaan testcontainers 1 menit terlalu pendek saat Docker kewalahan")
}

func TestConfigureReaperKeepsOperatorSetting(t *testing.T) {
	t.Run("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", func(t *testing.T) {
		t.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "10s")
		t.Setenv("RYUK_CONNECTION_TIMEOUT", "")

		ConfigureReaper()

		require.Equal(t, "10s", os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"))
	})
	t.Run("RYUK_CONNECTION_TIMEOUT tanpa awalan", func(t *testing.T) {
		t.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "")
		t.Setenv("RYUK_CONNECTION_TIMEOUT", "45s")

		ConfigureReaper()

		require.Equal(t, "", os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"), "nilai operator dihormati, tidak ditimpa")
		require.Equal(t, "45s", os.Getenv("RYUK_CONNECTION_TIMEOUT"))
	})
}
