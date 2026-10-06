package migrations_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Satu container PostgreSQL 18 untuk seluruh paket ini; dihentikan setelah semua tes selesai.
func TestMain(m *testing.M) {
	code := m.Run()
	testpg.Terminate()
	os.Exit(code)
}
