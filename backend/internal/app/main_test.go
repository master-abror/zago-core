package app_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Tes perakitan yang membuktikan perilaku dengan PostgreSQL nyata (authz_simulate_db_test.go)
// memakai container bersama dari testkit. Tes itu HARUS berada di paket eksternal app_test:
// testkit mengimpor testpg, testpg mengimpor dbmigrate, dan dbmigrate mengimpor app, sehingga
// tes internal (package app) yang mengimpor testkit membentuk siklus impor. Tes lain di paket
// ini tidak menyentuh Docker.
func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
