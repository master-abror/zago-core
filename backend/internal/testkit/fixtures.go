package testkit

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Fixture builder (docs/18 §7): tiap helper menghasilkan baris VALID MINIMAL yang memenuhi
// seluruh constraint docs/04, dengan opsi fungsional untuk variasi yang benar-benar dibutuhkan
// tes. Baris ditulis lewat role pemilik skema (db.Migrator) agar tidak bergantung pada grant
// runtime; yang diuji tetap kode aplikasi lewat db.App.

// Organization adalah baris organizations hasil fixture.
type Organization struct {
	ID   uuid.UUID
	Name string
	Slug string
}

// OrgOption mengubah organisasi yang akan dibuat.
type OrgOption func(*Organization)

// WithOrgName mengganti name.
func WithOrgName(name string) OrgOption { return func(o *Organization) { o.Name = name } }

// WithSlug mengganti slug (harus unik).
func WithSlug(slug string) OrgOption { return func(o *Organization) { o.Slug = slug } }

// NewOrganization menyisipkan satu organisasi aktif dengan slug unik.
func NewOrganization(t *testing.T, db *testpg.DB, opts ...OrgOption) Organization {
	t.Helper()
	oid := id.NewID()
	o := Organization{ID: oid, Name: "Org " + oid.String(), Slug: "org-" + oid.String()}
	for _, opt := range opts {
		opt(&o)
	}
	_, err := db.Migrator.Exec(context.Background(),
		`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
		o.ID.String(), o.Name, o.Slug)
	require.NoError(t, err, "fixture: gagal menyisipkan organisasi")
	return o
}

// User adalah baris users hasil fixture. OrganizationID kosong (uuid.Nil) bila tanpa organisasi.
type User struct {
	ID             uuid.UUID
	Email          string
	DisplayName    string
	OrganizationID uuid.UUID
}

type userSpec struct {
	email string
	org   *Organization
}

// UserOption mengubah pengguna yang akan dibuat.
type UserOption func(*userSpec)

// InOrganization membuat pengguna sekaligus keanggotaan organisasinya (status active), sehingga
// foreign key komposit milik tabel turunan langsung terpenuhi.
func InOrganization(o Organization) UserOption { return func(s *userSpec) { s.org = &o } }

// WithEmail mengganti email (harus huruf kecil dan unik).
func WithEmail(email string) UserOption {
	return func(s *userSpec) { s.email = strings.ToLower(email) }
}

// NewUser menyisipkan satu pengguna aktif dengan email unik.
func NewUser(t *testing.T, db *testpg.DB, opts ...UserOption) User {
	t.Helper()
	uid := id.NewID()
	spec := userSpec{email: uid.String() + "@example.test"}
	for _, opt := range opts {
		opt(&spec)
	}
	u := User{ID: uid, Email: spec.email, DisplayName: "User " + uid.String()}

	ctx := context.Background()
	_, err := db.Migrator.Exec(ctx,
		`INSERT INTO users (id, display_name, email, status) VALUES ($1, $2, $3, 'active')`,
		u.ID.String(), u.DisplayName, u.Email)
	require.NoError(t, err, "fixture: gagal menyisipkan pengguna")

	if spec.org != nil {
		u.OrganizationID = spec.org.ID
		_, err = db.Migrator.Exec(ctx,
			`INSERT INTO organization_memberships (id, organization_id, user_id, status, joined_at)
			 VALUES ($1, $2, $3, 'active', now())`,
			id.NewID().String(), spec.org.ID.String(), u.ID.String())
		require.NoError(t, err, "fixture: gagal menyisipkan keanggotaan organisasi")
	}
	return u
}
