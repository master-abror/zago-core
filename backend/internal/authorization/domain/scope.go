// Package domain memuat tipe inti Authorization Engine tanpa ketergantungan pada HTTP, DB, atau
// Redis (docs/06 §3, §13).
package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidScope menandai ScopeContext yang melanggar invarian: UUID nol, atau grup aktif tanpa
// organisasi.
var ErrInvalidScope = errors.New("authorization: scope tidak sah")

// ScopeContext adalah konteks scope satu request, diselesaikan per request (docs/06 §3).
//
// OrganizationID nil hanya sah untuk request yang bersifat tingkat platform; handler tenant wajib
// menolaknya. ActiveGroupID hanya terisi bila server SUDAH memverifikasi keanggotaan aktif
// (docs/06 §4.2); nilai mentah dari header tidak pernah masuk ke sini. Grup aktif tanpa organisasi
// tidak mungkin ada.
type ScopeContext struct {
	OrganizationID *uuid.UUID
	ActiveGroupID  *uuid.UUID
}

// NewScope membuat ScopeContext yang sah dan menyalin nilai yang ditunjuk argumen, sehingga
// pemanggil tidak bisa mengubah scope lewat pointer yang masih dipegangnya.
func NewScope(organizationID, activeGroupID *uuid.UUID) (ScopeContext, error) {
	if organizationID != nil && *organizationID == uuid.Nil {
		return ScopeContext{}, ErrInvalidScope
	}
	if activeGroupID != nil && (*activeGroupID == uuid.Nil || organizationID == nil) {
		return ScopeContext{}, ErrInvalidScope
	}
	return ScopeContext{OrganizationID: cloneID(organizationID), ActiveGroupID: cloneID(activeGroupID)}, nil
}

// IsPlatform melaporkan apakah request bersifat tingkat platform (tanpa organisasi).
func (s ScopeContext) IsPlatform() bool { return s.OrganizationID == nil }

// Tenant mengembalikan organisasi scope; ok=false untuk request tingkat platform.
func (s ScopeContext) Tenant() (uuid.UUID, bool) {
	if s.OrganizationID == nil {
		return uuid.Nil, false
	}
	return *s.OrganizationID, true
}

// Group mengembalikan grup aktif yang sudah terverifikasi; ok=false bila tidak ada.
func (s ScopeContext) Group() (uuid.UUID, bool) {
	if s.ActiveGroupID == nil {
		return uuid.Nil, false
	}
	return *s.ActiveGroupID, true
}

func cloneID(p *uuid.UUID) *uuid.UUID {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

type scopeKey struct{}

// WithScope menyimpan salinan scope pada context. Hanya middleware scope yang memanggilnya.
func WithScope(ctx context.Context, s ScopeContext) context.Context {
	return context.WithValue(ctx, scopeKey{}, ScopeContext{
		OrganizationID: cloneID(s.OrganizationID),
		ActiveGroupID:  cloneID(s.ActiveGroupID),
	})
}

// ScopeFromContext mengembalikan salinan scope request; ok=false bila middleware scope belum
// berjalan (pemanggil harus menolak, bukan menebak scope).
func ScopeFromContext(ctx context.Context) (ScopeContext, bool) {
	s, ok := ctx.Value(scopeKey{}).(ScopeContext)
	if !ok {
		return ScopeContext{}, false
	}
	return ScopeContext{OrganizationID: cloneID(s.OrganizationID), ActiveGroupID: cloneID(s.ActiveGroupID)}, true
}
