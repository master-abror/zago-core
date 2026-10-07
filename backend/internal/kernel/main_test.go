package kernel_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Satu container PostgreSQL 18 (dan Redis 8 bila dipakai) untuk seluruh paket ini.
func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
