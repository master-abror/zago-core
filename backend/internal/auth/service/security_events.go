package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
)

// maxSecurityEventsPage membatasi ukuran satu halaman di lapisan layanan (pengaman; batas yang
// dilihat klien ditetapkan transport lewat httpx.PageOptions).
const maxSecurityEventsPage = 1000

// SecurityEventInfo adalah satu baris riwayat event keamanan milik pemanggil (docs/05 §14,
// ADR-0020 butir 46). Metadata dan request_id sengaja TIDAK ada di sini.
type SecurityEventInfo struct {
	ID             uuid.UUID
	Type           string
	Result         string
	CreatedAt      time.Time
	IPAddress      string // "" bila tidak diketahui
	UserAgent      *string
	OrganizationID *uuid.UUID
	SessionID      *uuid.UUID
}

// EventPosition adalah kunci keyset (created_at, id) baris terakhir halaman sebelumnya.
type EventPosition struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListSecurityEvents mengembalikan event keamanan milik pemanggil, terbaru dulu. `before` nil =
// halaman pertama. user_id SELALU dari identitas: tidak ada jalan untuk membaca event pengguna lain.
// limit di luar 1..1000 dipotong ke rentang itu.
func (s *AuthService) ListSecurityEvents(ctx context.Context, id domain.Identity, limit int, before *EventPosition) ([]SecurityEventInfo, error) {
	limit = min(max(limit, 1), maxSecurityEventsPage)
	arg := repository.ListSecurityEventsByUserParams{UserID: id.UserID, PageSize: int32(limit)}
	if before != nil {
		at, bid := before.CreatedAt, before.ID
		arg.BeforeCreatedAt, arg.BeforeID = &at, &bid
	}
	rows, err := s.store.Q(ctx).ListSecurityEventsByUser(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("auth: baca event keamanan: %w", err)
	}
	out := make([]SecurityEventInfo, 0, len(rows))
	for _, r := range rows {
		info := SecurityEventInfo{
			ID: r.ID, Type: r.EventType, Result: r.Result, CreatedAt: r.CreatedAt,
			UserAgent: r.UserAgent, OrganizationID: r.OrganizationID, SessionID: r.SessionID,
		}
		if r.IpAddress != nil {
			info.IPAddress = r.IpAddress.String()
		}
		out = append(out, info)
	}
	return out, nil
}
