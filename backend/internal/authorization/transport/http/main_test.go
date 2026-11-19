package authzhttp_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// TestMain menghentikan container bersama (Redis untuk tes replay idempotensi) setelah semua tes.
func TestMain(m *testing.M) {
	os.Exit(testkit.Run(m))
}
