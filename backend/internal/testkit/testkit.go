// Package testkit adalah satu-satunya pintu infrastruktur tes integrasi M02+ (docs/18 §7):
// PostgreSQL 18 dan Redis 8 SUNGGUHAN lewat testcontainers (tanpa mock database), plus fixture
// builder yang menghasilkan baris valid minimal.
//
// PostgreSQL dibungkus dari internal/testpg (ADR-0009): tes skema M01 tetap memakai testpg
// langsung, tes lain memakai testkit, dan keduanya berbagi satu aturan skip/gagal tanpa Docker
// (REQUIRE_DOCKER=1 → gagal, bukan skip).
//
// Pemakaian di tiap paket tes yang membutuhkannya:
//
//	func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
package testkit

import (
	"strings"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Run menjalankan tes paket lalu menghentikan container bersama (PostgreSQL dan Redis).
// Dipanggil dari TestMain.
func Run(m *testing.M) int {
	code := m.Run()
	terminateRedis()
	testpg.Terminate()
	return code
}

// NewDB membuat database baru yang SUDAH dimigrasi sampai head, lengkap dengan pool untuk tiga
// role aplikasi (db.App = app_user, db.Maintenance, db.Migrator). Dihapus otomatis saat tes selesai.
func NewDB(t *testing.T) *testpg.DB {
	t.Helper()
	return testpg.Shared(t).NewDB(t)
}

// PostgresURL mengembalikan URL postgres:// (skema pgx runtime, bukan pgx5:// milik cmd/migrate)
// untuk role tertentu ke database tes, siap dipakai kernel.NewPool.
func PostgresURL(db *testpg.DB, role string) string {
	return "postgres://" + strings.TrimPrefix(db.RoleURL(role), "pgx5://")
}
