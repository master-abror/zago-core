package httpx_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Redis 8 bersama (testcontainers) untuk tes idempotency/rate limit; dihentikan setelah paket selesai.
func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
