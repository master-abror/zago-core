package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
)

// GroupMembershipChecker memverifikasi, terhadap sumber otoritatif (PostgreSQL), bahwa user adalah
// anggota AKTIF grup yang AKTIF dan tidak dihapus di organisasi tertentu (docs/06 §4.2).
// Galat berarti verifikasi tidak bisa dilakukan; (false, nil) berarti terbukti bukan anggota.
type GroupMembershipChecker interface {
	IsActiveGroupMember(ctx context.Context, userID, organizationID, groupID uuid.UUID) (bool, error)
}

// ScopeResolver menyelesaikan ScopeContext tiap request dari Identity (docs/06 §3, §4).
type ScopeResolver struct {
	groups GroupMembershipChecker
	log    *slog.Logger
}

// NewScopeResolver membuat ScopeResolver; log boleh nil (slog.Default).
func NewScopeResolver(groups GroupMembershipChecker, log *slog.Logger) (*ScopeResolver, error) {
	if groups == nil {
		return nil, errors.New("authorization: GroupMembershipChecker wajib")
	}
	if log == nil {
		log = slog.Default()
	}
	return &ScopeResolver{groups: groups, log: log}, nil
}

// Resolve menghasilkan scope request. Organisasi selalu berasal dari Identity (tetap selama sesi),
// tidak pernah dari request. groupHeader adalah SEMUA nilai header X-Active-Group-ID; header
// hanyalah petunjuk (docs/06 §4.2): grup aktif terisi hanya bila lolos verifikasi ke database pada
// request ini. Header yang cacat, ganda, nol, atau gagal verifikasi dibuang tanpa galat, sehingga
// aksi ber-scope grup ditolak oleh engine, bukan memilihkan grup untuk user. Galat dikembalikan
// hanya bila verifikasi tidak bisa dilakukan (infrastruktur) atau Identity tidak sah; pemanggil
// harus menolak request (fail-closed), bukan melanjutkan tanpa grup.
func (r *ScopeResolver) Resolve(ctx context.Context, id authdomain.Identity, groupHeader []string) (domain.ScopeContext, error) {
	scope, err := domain.NewScope(id.OrganizationID, nil)
	if err != nil {
		return domain.ScopeContext{}, err
	}
	if len(groupHeader) == 0 {
		return scope, nil
	}
	if scope.IsPlatform() {
		r.discard(ctx, "identitas tanpa organisasi")
		return scope, nil
	}
	groupID, reason := parseGroupHeader(groupHeader)
	if reason != "" {
		r.discard(ctx, reason)
		return scope, nil
	}
	ok, err := r.groups.IsActiveGroupMember(ctx, id.UserID, *id.OrganizationID, groupID)
	if err != nil {
		return domain.ScopeContext{}, fmt.Errorf("authorization: verifikasi grup aktif: %w", err)
	}
	if !ok {
		r.discard(ctx, "bukan anggota aktif grup aktif di organisasi sesi")
		return scope, nil
	}
	return domain.NewScope(id.OrganizationID, &groupID)
}

// parseGroupHeader menerima tepat satu nilai berbentuk UUID kanonik (36 karakter berstrip) yang
// bukan UUID nol. Bentuk lain yang diterima uuid.Parse (urn:, kurung kurawal, tanpa strip) ditolak
// supaya satu grup tidak punya banyak representasi. Mengembalikan alasan penolakan bila gagal.
func parseGroupHeader(values []string) (uuid.UUID, string) {
	if len(values) != 1 {
		return uuid.Nil, "header ganda"
	}
	raw := values[0]
	if len(raw) != 36 {
		return uuid.Nil, "bukan UUID kanonik"
	}
	gid, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, "bukan UUID"
	}
	if gid == uuid.Nil {
		return uuid.Nil, "UUID nol"
	}
	return gid, ""
}

// discard mencatat (level debug) bahwa header dibuang, hanya dengan alasan: nilai header berasal
// dari klien dan tidak boleh masuk log.
func (r *ScopeResolver) discard(ctx context.Context, reason string) {
	r.log.DebugContext(ctx, "X-Active-Group-ID dibuang", "reason", reason)
}
