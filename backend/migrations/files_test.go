package migrations_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/migrations"
)

// Urutan resmi docs/04 §14. Menambah migrasi = menambah baris di sini (dan nomor baru; migrasi
// yang sudah di-tag tak pernah diubah atau dinomori ulang). Tes ini tak butuh Docker.
var wantOrder = []string{
	"001_common_functions", "002_platforms", "003_organizations", "004_users",
	"005_organization_memberships", "006_groups", "007_group_memberships", "008_modules",
	"009_module_dependencies", "010_module_installations", "011_permissions", "012_roles",
	"013_role_permissions", "014_role_assignments", "015_policies", "016_settings",
	"017_credentials", "018_sessions", "019_security_events", "020_invitations",
	"021_password_reset_tokens", "022_notifications", "023_inbox_items", "024_conversations",
	"025_conversation_members", "026_messages", "027_message_reads", "028_message_reactions",
	"029_attachments", "030_activities", "031_system_logs", "032_outbox_events", "033_grants",
}

func TestMigrationFilesMatchDocumentedOrderAndArePaired(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	require.NoError(t, err)

	have := map[string]bool{}
	for _, e := range entries {
		have[e.Name()] = true
	}
	for _, name := range wantOrder {
		require.True(t, have[name+".up.sql"], "hilang: %s.up.sql", name)
		require.True(t, have[name+".down.sql"], "hilang: %s.down.sql", name)
	}
	// Tak ada berkas di luar daftar (mis. nomor loncat atau berkas liar).
	require.Len(t, have, 2*len(wantOrder), "ada berkas migrasi di luar docs/04 §14")
	for name := range have {
		require.Regexp(t, `^\d{3}_[a-z0-9_]+\.(up|down)\.sql$`, name)
	}
}

var createTable = regexp.MustCompile(`(?i)CREATE TABLE\s+(\w+)\s*\(`)

// Konvensi docs/04 §4: tabel ber-updated_at memanggil attach_updated_at di migrasinya sendiri.
// (Pemindai katalog di schema_test.go membuktikan hasilnya di database; ini menangkapnya di sumber.)
func TestMigrationsWithUpdatedAtCallAttach(t *testing.T) {
	for _, name := range wantOrder {
		b, err := fs.ReadFile(migrations.FS, name+".up.sql")
		require.NoError(t, err)
		sql := string(b)
		for _, m := range createTable.FindAllStringSubmatch(sql, -1) {
			table := m[1]
			body := sql[strings.Index(sql, m[0]):]
			if end := strings.Index(body, ");\n"); end >= 0 {
				body = body[:end]
			}
			if strings.Contains(body, "updated_at") {
				require.Contains(t, sql, "attach_updated_at('"+table+"')", "%s: %s punya updated_at tanpa attach_updated_at", name, table)
			}
		}
	}
}

func TestEveryUpHasNoSecretsOrDevPasswords(t *testing.T) {
	for _, name := range wantOrder {
		b, err := fs.ReadFile(migrations.FS, name+".up.sql")
		require.NoError(t, err)
		require.NotContains(t, strings.ToLower(string(b)), "password '", name)
	}
}
