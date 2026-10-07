package infra_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// testkit.Run menghentikan container bersama (PostgreSQL) yang dipakai tes worker.
func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
