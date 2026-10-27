package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
)

// Event keamanan manajemen sesi (docs/05 §11.1, §14).
const (
	EventSessionRevoked        = "auth.session.revoked"
	EventSessionsRevokedOthers = "auth.sessions.revoked_others"
)

// SessionInfo adalah sesi aktif milik pengguna untuk GET /security/sessions. Tidak membawa
// token maupun hash token.
type SessionInfo struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	LastActivityAt time.Time
	ExpiresAt      time.Time
	IPAddress      string // "" bila tidak diketahui
	UserAgent      *string
	OrganizationID *uuid.UUID
	AuthLevel      string
	Current        bool // sesi yang sedang dipakai pemanggil
}

// ListSessions mengembalikan sesi aktif pemanggil (terbaru dulu), dengan penanda sesi saat ini.
func (s *AuthService) ListSessions(ctx context.Context, id domain.Identity) ([]SessionInfo, error) {
	rows, err := s.sessions.List(ctx, id.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		info := SessionInfo{
			ID: r.ID, CreatedAt: r.CreatedAt, LastActivityAt: r.LastActivityAt, ExpiresAt: r.ExpiresAt,
			UserAgent: r.UserAgent, OrganizationID: r.OrganizationID, AuthLevel: r.AuthLevel,
			Current: r.ID == id.SessionID,
		}
		if r.IpAddress != nil {
			info.IPAddress = r.IpAddress.String()
		}
		out = append(out, info)
	}
	return out, nil
}

// RevokeSession mencabut satu sesi milik pemanggil (boleh sesi saat ini). Sesi yang tidak ada,
// milik orang lain, atau sudah dicabut menjadi domain.ErrSessionNotFound tanpa efek samping.
func (s *AuthService) RevokeSession(ctx context.Context, id domain.Identity, target uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.sessions.Revoke(ctx, id.UserID, target, ReasonRevokedByUser); err != nil {
			return err
		}
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventSessionRevoked, Result: eventResultSuccess, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &target,
			Metadata: map[string]any{"reason": ReasonRevokedByUser, "current": target == id.SessionID},
		}); err != nil {
			return fmt.Errorf("auth: catat event pencabutan sesi: %w", err)
		}
		s.record(ctx, id.UserID, id.OrganizationID, "auth.session.revoke", target)
		return nil
	})
}

// RevokeOtherSessions mencabut semua sesi aktif pemanggil selain yang sedang dipakai dan
// mengembalikan jumlahnya. Tidak ada yang dicabut = tidak ada event (bukan kejadian keamanan).
func (s *AuthService) RevokeOtherSessions(ctx context.Context, id domain.Identity) (int, error) {
	var n int
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		count, err := s.sessions.RevokeOthers(ctx, id.UserID, id.SessionID, ReasonRevokeOthers)
		if err != nil {
			return err
		}
		n = count
		if count == 0 {
			return nil
		}
		keep := id.SessionID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventSessionsRevokedOthers, Result: eventResultSuccess, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &keep,
			Metadata: map[string]any{"reason": ReasonRevokeOthers, "count": count},
		}); err != nil {
			return fmt.Errorf("auth: catat event pencabutan sesi lain: %w", err)
		}
		s.record(ctx, id.UserID, id.OrganizationID, "auth.sessions.revoke_others", keep)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
