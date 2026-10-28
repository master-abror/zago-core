package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/security"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Event ganti password (docs/05 §13.1, §14; ADR-0020 butir 43–45). Nama yang sama dipakai untuk
// security event dan event outbox (tabel berbeda, pembaca berbeda).
const (
	EventPasswordChanged      = "auth.password.changed"
	EventPasswordChangeFailed = "auth.password.change_failed"

	activityPasswordChange = "auth.password.change"
	aggregateUser          = "user"
)

// ChangePasswordInput adalah masukan ChangePassword. Keduanya tidak pernah dicatat atau dilog.
type ChangePasswordInput struct {
	Current string
	New     string
}

// ChangePasswordResult adalah hasil ganti password: sesi pengganti (Issued.Token hanya boleh
// diserahkan ke cookie) dan jumlah sesi LAIN yang dicabut.
type ChangePasswordResult struct {
	Issued        Issued
	RevokedOthers int
}

// passwordChangedPayload adalah isi event outbox auth.password.changed: tanpa email, password,
// hash, atau token (ADR-0020 butir 43).
type passwordChangedPayload struct {
	UserID               uuid.UUID `json:"user_id"`
	RevokedOtherSessions int       `json:"revoked_other_sessions"`
}

// ChangePassword mengganti password pemanggil (docs/05 §13.1, ADR-0020 butir 42–45).
//
// Urutan: password saat ini diverifikasi (salah = ErrInvalidCredentials, tidak ada yang berubah selain
// event kegagalan) → kebijakan password baru (security.ErrPasswordTooShort/TooLong) → password baru
// tidak boleh sama (ErrSamePassword) → hash baru dihitung DI LUAR transaksi → SATU transaksi: ganti
// hash secara compare-and-swap, rotasi sesi pemanggil, cabut semua sesi lain, event keamanan, activity,
// dan event outbox. Gagal di langkah mana pun = rollback total. Efek Redis/Pub/Sub berjalan setelah commit.
//
// Dua perubahan bersamaan dengan password lama yang sama: hanya satu yang commit; yang kalah mendapat
// ErrInvalidCredentials (password saat ini yang ia kirim sudah tak berlaku) tanpa efek samping. Sesi
// yang sudah dicabut mendapat ErrUnauthenticated dan password tidak berubah.
func (s *AuthService) ChangePassword(ctx context.Context, id domain.Identity, in ChangePasswordInput) (ChangePasswordResult, error) {
	cred, err := s.store.Q(ctx).GetActivePasswordCredential(ctx, id.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChangePasswordResult{}, domain.ErrInvalidCredentials // tidak ada password yang bisa diganti
	}
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("auth: baca credential: %w", err)
	}

	switch err := s.hasher.Compare(ctx, cred.SecretHash, in.Current); {
	case err == nil:
	case errors.Is(err, security.ErrMismatch), errors.Is(err, security.ErrPasswordTooLong):
		s.recordPasswordChangeFailed(ctx, id, "bad_current_password")
		return ChangePasswordResult{}, domain.ErrInvalidCredentials
	case errors.Is(err, security.ErrInvalidHash):
		// Data rusak, bukan password salah: terlihat di log; klien tetap melihat kegagalan biasa.
		s.log.ErrorContext(ctx, "hash password tersimpan tidak valid", "user_id", id.UserID)
		s.recordPasswordChangeFailed(ctx, id, "bad_current_password")
		return ChangePasswordResult{}, domain.ErrInvalidCredentials
	default:
		return ChangePasswordResult{}, fmt.Errorf("auth: verifikasi password saat ini: %w", err)
	}

	if err := s.policy.Check(in.New); err != nil {
		return ChangePasswordResult{}, err
	}
	if in.New == in.Current {
		return ChangePasswordResult{}, domain.ErrSamePassword
	}

	newHash, err := s.hasher.Hash(ctx, in.New)
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("auth: hash password baru: %w", err)
	}
	if ctx.Err() != nil {
		return ChangePasswordResult{}, fmt.Errorf("auth: ganti password dibatalkan: %w", ctx.Err())
	}

	var out ChangePasswordResult
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		n, err := s.store.Q(ctx).RehashPasswordIfUnchanged(ctx, repository.RehashPasswordIfUnchangedParams{
			CredentialID: cred.ID, OldHash: cred.SecretHash, NewHash: newHash,
		})
		if err != nil {
			return fmt.Errorf("auth: ganti hash password: %w", err)
		}
		if n == 0 {
			return domain.ErrInvalidCredentials // diganti bersamaan: password saat ini sudah tak berlaku
		}

		ip, ua := clientInfo(ctx)
		iss, err := s.sessions.Rotate(ctx, id, RotateOptions{Reason: ReasonPasswordChanged, IP: ip, UserAgent: ua})
		if err != nil {
			return err
		}
		revoked, err := s.sessions.RevokeOthers(ctx, id.UserID, iss.Session.ID, ReasonPasswordChanged)
		if err != nil {
			return err
		}

		sid := iss.Session.ID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventPasswordChanged, Result: eventResultSuccess, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &sid,
			Metadata: map[string]any{"previous_session_id": id.SessionID.String(), "revoked_other_sessions": revoked},
		}); err != nil {
			return fmt.Errorf("auth: catat event ganti password: %w", err)
		}
		s.recordResource(ctx, id.UserID, id.OrganizationID, activityPasswordChange, "user", id.UserID)
		s.publishPasswordChanged(ctx, id, revoked)

		out = ChangePasswordResult{Issued: iss, RevokedOthers: revoked}
		return nil
	})
	if err != nil {
		return ChangePasswordResult{}, err
	}
	return out, nil
}

// publishPasswordChanged menulis event outbox pada transaksi di ctx. Gagal = transaksi rollback-only
// (Bus.Publish memanggil FailTx), jadi password tidak berubah tanpa event (ADR-0020 butir 43).
func (s *AuthService) publishPasswordChanged(ctx context.Context, id domain.Identity, revoked int) {
	if s.bus == nil {
		return
	}
	ev, err := modulesdk.NewEvent(EventPasswordChanged, passwordChangedPayload{UserID: id.UserID, RevokedOtherSessions: revoked})
	if err != nil {
		kernel.FailTx(ctx, fmt.Errorf("auth: bentuk event ganti password: %w", err))
		return
	}
	ev.AggregateType = aggregateUser
	ev.AggregateID = id.UserID
	if id.OrganizationID != nil {
		ev.OrganizationID = *id.OrganizationID
	}
	s.bus.Publish(ctx, ev)
}

// recordPasswordChangeFailed mencatat password saat ini yang salah: sinyal pencurian sesi (penyerang
// menebak password lewat sesi curian). Best-effort dan tidak membatalkan (pola recordFailure). TIDAK
// menaikkan penghitung kunci akun (ADR-0020 butir 45).
func (s *AuthService) recordPasswordChangeFailed(ctx context.Context, id domain.Identity, reason string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	sid := id.SessionID
	if err := s.insertEvent(wctx, securityEvent{
		Type: EventPasswordChangeFailed, Result: eventResultFailure, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &sid,
		Metadata: map[string]any{"reason": reason},
	}); err != nil {
		s.log.WarnContext(ctx, "gagal mencatat event ganti password gagal", "error", err)
	}
}
