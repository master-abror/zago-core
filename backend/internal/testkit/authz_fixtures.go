package testkit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Fixture otorisasi (docs/18 §7): role, permission, relasi, dan assignment valid minimal.
// Seperti fixture lain, baris ditulis lewat db.Migrator.

// Role adalah baris roles hasil fixture. OrganizationID = uuid.Nil untuk role sistem (global).
type Role struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Slug           string
}

type roleSpec struct {
	org    *Organization
	status string
}

// RoleOption mengubah role yang akan dibuat.
type RoleOption func(*roleSpec)

// RoleInOrganization membuat role milik organisasi (role_type 'custom') alih-alih role sistem.
func RoleInOrganization(o Organization) RoleOption { return func(s *roleSpec) { s.org = &o } }

// RoleStatus mengganti status role ('active' atau 'archived').
func RoleStatus(status string) RoleOption { return func(s *roleSpec) { s.status = status } }

// NewRole menyisipkan satu role aktif dengan slug unik; default-nya role sistem global.
func NewRole(t *testing.T, db *testpg.DB, opts ...RoleOption) Role {
	t.Helper()
	rid := id.NewID()
	spec := roleSpec{status: "active"}
	for _, opt := range opts {
		opt(&spec)
	}
	r := Role{ID: rid, Slug: "role-" + rid.String()}

	var err error
	if spec.org == nil {
		_, err = db.Migrator.Exec(context.Background(),
			`INSERT INTO roles (id, name, slug, role_type, status) VALUES ($1, $2, $3, 'system', $4)`,
			r.ID.String(), "Role "+rid.String(), r.Slug, spec.status)
	} else {
		r.OrganizationID = spec.org.ID
		_, err = db.Migrator.Exec(context.Background(),
			`INSERT INTO roles (id, organization_id, name, slug, role_type, status) VALUES ($1, $2, $3, $4, 'custom', $5)`,
			r.ID.String(), spec.org.ID.String(), "Role "+rid.String(), r.Slug, spec.status)
	}
	require.NoError(t, err, "fixture: gagal menyisipkan role")
	return r
}

// Permission adalah baris permissions hasil fixture.
type Permission struct {
	ID   uuid.UUID
	Code string
}

// EnsurePermission menyisipkan permission core (is_system) untuk code, atau memakai ulang baris
// yang sudah ada. code harus berbentuk "resource.action".
func EnsurePermission(t *testing.T, db *testpg.DB, code string) Permission {
	t.Helper()
	dot := strings.LastIndex(code, ".")
	require.Greater(t, dot, 0, "fixture: kode permission harus berbentuk resource.action")

	var pid uuid.UUID
	err := db.Migrator.QueryRow(context.Background(),
		`INSERT INTO permissions (id, resource, action, code, is_system) VALUES ($1, $2, $3, $4, true)
		 ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
		 RETURNING id`,
		id.NewID().String(), code[:dot], code[dot+1:], code).Scan(&pid)
	require.NoError(t, err, "fixture: gagal menyisipkan permission")
	return Permission{ID: pid, Code: code}
}

// GrantPermission menautkan permission ke role (role_permissions).
func GrantPermission(t *testing.T, db *testpg.DB, r Role, p Permission) {
	t.Helper()
	_, err := db.Migrator.Exec(context.Background(),
		`INSERT INTO role_permissions (id, role_id, permission_id) VALUES ($1, $2, $3)`,
		id.NewID().String(), r.ID.String(), p.ID.String())
	require.NoError(t, err, "fixture: gagal menautkan permission ke role")
}

// RoleWith membuat role (dengan opts) yang sekaligus memegang semua code yang diminta.
func RoleWith(t *testing.T, db *testpg.DB, codes []string, opts ...RoleOption) Role {
	t.Helper()
	r := NewRole(t, db, opts...)
	for _, c := range codes {
		GrantPermission(t, db, r, EnsurePermission(t, db, c))
	}
	return r
}

// Assignment adalah baris role_assignments hasil fixture.
type Assignment struct {
	ID uuid.UUID
}

// AssignmentSpec menjelaskan satu assignment. OrganizationID nil untuk scope platform (CHECK
// ck_role_assignments_org); ScopeID nil untuk scope platform, own, dan organisasi seluruhnya.
type AssignmentSpec struct {
	Role           Role
	SubjectType    string // user | group | organization | service
	SubjectID      uuid.UUID
	ScopeType      string // platform | organization | group | resource | own
	ScopeID        *uuid.UUID
	OrganizationID *uuid.UUID
	ExpiresAt      *time.Time
	Revoked        bool
}

// AssignRole menyisipkan satu assignment sesuai spec.
func AssignRole(t *testing.T, db *testpg.DB, spec AssignmentSpec) Assignment {
	t.Helper()
	aid := id.NewID()
	var revokedAt *time.Time
	if spec.Revoked {
		now := time.Now()
		revokedAt = &now
	}
	_, err := db.Migrator.Exec(context.Background(),
		`INSERT INTO role_assignments
		   (id, organization_id, role_id, subject_type, subject_id, scope_type, scope_id, expires_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		aid.String(), idArg(spec.OrganizationID), spec.Role.ID.String(), spec.SubjectType,
		spec.SubjectID.String(), spec.ScopeType, idArg(spec.ScopeID), spec.ExpiresAt, revokedAt)
	require.NoError(t, err, "fixture: gagal menyisipkan role assignment")
	return Assignment{ID: aid}
}

// idArg mengubah *uuid.UUID menjadi argumen kueri: nil menjadi NULL, selain itu teks UUID.
func idArg(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
}
