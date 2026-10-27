package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/security"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Nama event keamanan (docs/05 §14). result mengikuti CHECK skema: success | failure.
const (
	EventLoginSuccess = "auth.login.success"
	EventLoginFailed  = "auth.login.failed"
	EventLogout       = "auth.logout"

	EventSessionRefreshed     = "auth.session.refreshed"
	EventOrganizationSwitched = "auth.organization.switched"

	eventResultSuccess = "success"
	eventResultFailure = "failure"

	revokeReasonLogout             = "logout"
	revokeReasonSwitchOrganization = "switch_organization"
)

// AuthService adalah use case autentikasi di atas SessionService: login, logout, profil.
// Aturan keras (docs/05 §3, §4, §18): verifikasi Argon2id SELALU berjalan (terhadap hash dummy
// bila akun tidak ada), status akun baru diperiksa setelah password benar, dan sesi + event
// keamanan + activity ditulis dalam SATU transaksi.
type AuthService struct {
	store    *repository.Store
	tx       *kernel.TxManager
	sessions *SessionService
	hasher   security.PasswordHasher
	audit    modulesdk.AuditRecorder
	log      *slog.Logger
}

// AuthDeps adalah dependensi AuthService. Audit dan Log opsional.
type AuthDeps struct {
	Store    *repository.Store
	Tx       *kernel.TxManager
	Sessions *SessionService
	Hasher   security.PasswordHasher
	Audit    modulesdk.AuditRecorder
	Log      *slog.Logger
}

// NewAuthService membuat AuthService; Store, Tx, Sessions, dan Hasher wajib.
func NewAuthService(d AuthDeps) (*AuthService, error) {
	switch {
	case d.Store == nil:
		return nil, errors.New("auth: AuthDeps.Store wajib")
	case d.Tx == nil:
		return nil, errors.New("auth: AuthDeps.Tx wajib")
	case d.Sessions == nil:
		return nil, errors.New("auth: AuthDeps.Sessions wajib")
	case d.Hasher == nil:
		return nil, errors.New("auth: AuthDeps.Hasher wajib")
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	return &AuthService{store: d.Store, tx: d.Tx, sessions: d.Sessions, hasher: d.Hasher, audit: d.Audit, log: log}, nil
}

// NormalizeEmail menyeragamkan email untuk pencarian (skema mewajibkan huruf kecil).
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// LoginInput adalah masukan Login. Asal klien (IP, User-Agent) dibaca dari kernel.Client pada ctx.
type LoginInput struct {
	Email    string
	Password string
}

// LoginUser adalah ringkasan pengguna yang baru login.
type LoginUser struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
}

// LoginResult adalah hasil login yang berhasil. Issued.Token hanya boleh diserahkan ke cookie.
type LoginResult struct {
	Issued         Issued
	User           LoginUser
	OrganizationID *uuid.UUID // nil = identitas tingkat platform
}

// Login memverifikasi kredensial dan menerbitkan sesi baru. Kesalahan domain: ErrInvalidCredentials,
// ErrAccountSuspended, ErrAccountLocked; selain itu adalah kegagalan infrastruktur.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	q := s.store.Q(ctx)

	user, err := q.GetUserForLoginByEmail(ctx, NormalizeEmail(in.Email))
	known := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LoginResult{}, fmt.Errorf("auth: cari user: %w", err)
	}
	var cred repository.GetActivePasswordCredentialRow
	if known {
		cred, err = q.GetActivePasswordCredential(ctx, user.ID)
		known = err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return LoginResult{}, fmt.Errorf("auth: cari credential: %w", err)
		}
	}

	if !known {
		// Email tak dikenal atau tanpa credential: tetap jalankan verifikasi penuh (waktu setara).
		if err := s.hasher.CompareDummy(ctx, in.Password); err != nil &&
			!errors.Is(err, security.ErrMismatch) && !errors.Is(err, security.ErrPasswordTooLong) {
			return LoginResult{}, fmt.Errorf("auth: verifikasi dummy: %w", err)
		}
		return LoginResult{}, domain.ErrInvalidCredentials
	}

	switch err := s.hasher.Compare(ctx, cred.SecretHash, in.Password); {
	case err == nil:
	case errors.Is(err, security.ErrMismatch), errors.Is(err, security.ErrPasswordTooLong):
		s.recordFailure(ctx, user.ID, "bad_password")
		return LoginResult{}, domain.ErrInvalidCredentials
	case errors.Is(err, security.ErrInvalidHash):
		// Data rusak, bukan password salah: terlihat di log, tetapi klien tetap melihat 401 biasa.
		s.log.ErrorContext(ctx, "hash password tersimpan tidak valid", "user_id", user.ID)
		s.recordFailure(ctx, user.ID, "bad_password")
		return LoginResult{}, domain.ErrInvalidCredentials
	default:
		return LoginResult{}, fmt.Errorf("auth: verifikasi password: %w", err)
	}

	// Password sah: baru sekarang status akun boleh terlihat (docs/05 §18).
	if statusErr := loginStatusError(user.Status); statusErr != nil {
		s.recordFailure(ctx, user.ID, "status_"+user.Status)
		return LoginResult{}, statusErr
	}

	newHash := s.rehash(ctx, cred.SecretHash, in.Password)
	if ctx.Err() != nil {
		return LoginResult{}, fmt.Errorf("auth: login dibatalkan: %w", ctx.Err())
	}

	var out LoginResult
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.store.Q(ctx)

		var orgID *uuid.UUID
		org, err := q.GetDefaultActiveOrganizationID(ctx, user.ID)
		switch {
		case err == nil:
			orgID = &org
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("auth: organisasi awal: %w", err)
		}

		if newHash != "" {
			if _, err := q.RehashPasswordIfUnchanged(ctx, repository.RehashPasswordIfUnchangedParams{
				CredentialID: cred.ID, OldHash: cred.SecretHash, NewHash: newHash,
			}); err != nil {
				return fmt.Errorf("auth: rehash password: %w", err)
			}
		} else if err := q.TouchCredentialUsed(ctx, cred.ID); err != nil {
			return fmt.Errorf("auth: sentuh credential: %w", err)
		}
		if err := q.SetUserLastLogin(ctx, user.ID); err != nil {
			return fmt.Errorf("auth: catat login terakhir: %w", err)
		}

		ip, ua := clientInfo(ctx)
		iss, err := s.sessions.Create(ctx, CreateParams{
			UserID: user.ID, OrganizationID: orgID, AuthLevel: domain.AuthLevelPassword, IP: ip, UserAgent: ua,
		})
		if err != nil {
			return fmt.Errorf("auth: buat sesi: %w", err)
		}

		sid := iss.Session.ID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventLoginSuccess, Result: eventResultSuccess, UserID: user.ID, OrgID: orgID, SessionID: &sid,
		}); err != nil {
			return fmt.Errorf("auth: catat event login: %w", err)
		}
		s.record(ctx, user.ID, orgID, "auth.login", sid)

		out = LoginResult{
			Issued:         iss,
			User:           LoginUser{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName},
			OrganizationID: orgID,
		}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	return out, nil
}

// Logout mencabut sesi pemanggil. Idempoten: sesi yang sudah tidak ada bukan kesalahan.
func (s *AuthService) Logout(ctx context.Context, id domain.Identity) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		err := s.sessions.Revoke(ctx, id.UserID, id.SessionID, revokeReasonLogout)
		if errors.Is(err, domain.ErrSessionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		sid := id.SessionID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventLogout, Result: eventResultSuccess, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &sid,
		}); err != nil {
			return fmt.Errorf("auth: catat event logout: %w", err)
		}
		s.record(ctx, id.UserID, id.OrganizationID, "auth.logout", sid)
		return nil
	})
}

// Refresh mengganti token sesi pemanggil (docs/05 §12.2, ADR-0020): sesi lama dicabut, sesi baru
// lahir dengan organisasi dan batas absolut yang SAMA (refresh tidak memperpanjang umur sesi)
// dan idle clock mulai ulang. Token CSRF lama tidak berlaku lagi. Sesi yang sudah tidak sah
// menghasilkan domain.ErrUnauthenticated.
func (s *AuthService) Refresh(ctx context.Context, id domain.Identity) (Issued, error) {
	var out Issued
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		ip, ua := clientInfo(ctx)
		iss, err := s.sessions.Rotate(ctx, id, RotateOptions{IP: ip, UserAgent: ua})
		if err != nil {
			return err
		}
		sid := iss.Session.ID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventSessionRefreshed, Result: eventResultSuccess, UserID: id.UserID, OrgID: id.OrganizationID, SessionID: &sid,
			Metadata: map[string]any{"previous_session_id": id.SessionID.String()},
		}); err != nil {
			return fmt.Errorf("auth: catat event refresh: %w", err)
		}
		out = iss
		return nil
	})
	if err != nil {
		return Issued{}, err
	}
	return out, nil
}

// SwitchOrganization memindahkan sesi ke organisasi lain tempat user menjadi anggota aktif
// (docs/05 §12.2): keanggotaan diperiksa dan token dirotasi dalam SATU transaksi, jadi sesi tidak
// pernah menunjuk organisasi yang bukan haknya. Bukan anggota (atau organisasi tak ada/tak aktif)
// = domain.ErrNotOrganizationMember tanpa efek samping.
func (s *AuthService) SwitchOrganization(ctx context.Context, id domain.Identity, orgID uuid.UUID) (Issued, error) {
	var out Issued
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		member, err := s.store.Q(ctx).HasActiveMembership(ctx, repository.HasActiveMembershipParams{UserID: id.UserID, OrganizationID: orgID})
		if err != nil {
			return fmt.Errorf("auth: periksa keanggotaan: %w", err)
		}
		if !member {
			return domain.ErrNotOrganizationMember
		}

		ip, ua := clientInfo(ctx)
		iss, err := s.sessions.Rotate(ctx, id, RotateOptions{
			Reason: revokeReasonSwitchOrganization, ChangeOrganization: true, OrganizationID: &orgID, IP: ip, UserAgent: ua,
		})
		if err != nil {
			return err
		}

		meta := map[string]any{"previous_session_id": id.SessionID.String()}
		if id.OrganizationID != nil {
			meta["from_organization_id"] = id.OrganizationID.String()
		}
		sid := iss.Session.ID
		if err := s.insertEvent(ctx, securityEvent{
			Type: EventOrganizationSwitched, Result: eventResultSuccess, UserID: id.UserID, OrgID: &orgID, SessionID: &sid, Metadata: meta,
		}); err != nil {
			return fmt.Errorf("auth: catat event pindah organisasi: %w", err)
		}
		s.record(ctx, id.UserID, &orgID, "auth.switch_organization", sid)
		out = iss
		return nil
	})
	if err != nil {
		return Issued{}, err
	}
	return out, nil
}

// Profile adalah data diri pengguna untuk GET /auth/me.
type Profile struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Username    *string
	Locale      string
	Timezone    string
	LastLoginAt *time.Time
}

// Profile membaca data diri pengguna. Pengguna yang sudah tidak ada dianggap tidak terautentikasi.
func (s *AuthService) Profile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	row, err := s.store.Q(ctx).GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return Profile{}, fmt.Errorf("auth: baca profil: %w", err)
	}
	return Profile{
		ID: row.ID, Email: row.Email, DisplayName: row.DisplayName, Username: row.Username,
		Locale: row.Locale, Timezone: row.Timezone, LastLoginAt: row.LastLoginAt,
	}, nil
}

// loginStatusError memetakan users.status (setelah password benar). Status yang tidak boleh
// dibedakan dari "kredensial salah" (pending, deleted) menjadi ErrInvalidCredentials.
func loginStatusError(status string) error {
	switch status {
	case "active":
		return nil
	case "suspended", "deactivated":
		return domain.ErrAccountSuspended
	case "locked":
		return domain.ErrAccountLocked
	default:
		return domain.ErrInvalidCredentials
	}
}

// rehash mengembalikan hash baru bila parameter hash tersimpan basi, "" bila tidak perlu atau
// gagal (login tetap berhasil; rehash dicoba lagi pada login berikutnya).
func (s *AuthService) rehash(ctx context.Context, oldHash, password string) string {
	if !s.hasher.NeedsRehash(oldHash) {
		return ""
	}
	h, err := s.hasher.Hash(ctx, password)
	if err != nil {
		if ctx.Err() == nil {
			s.log.WarnContext(ctx, "rehash password gagal; dicoba lagi pada login berikutnya", "error", err)
		}
		return ""
	}
	return h
}

type securityEvent struct {
	Type      string
	Result    string
	UserID    uuid.UUID
	OrgID     *uuid.UUID
	SessionID *uuid.UUID
	Metadata  map[string]any
}

// insertEvent menulis event keamanan pada transaksi di ctx (bila ada). Metadata tidak boleh
// berisi password, token, atau email.
func (s *AuthService) insertEvent(ctx context.Context, e securityEvent) error {
	meta := []byte("{}")
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return err
		}
		meta = b
	}
	ip, ua := clientInfo(ctx)
	var rid *string
	if v := kernel.RequestIDFromContext(ctx); v != "" {
		rid = &v
	}
	uid := e.UserID
	return s.store.Q(ctx).InsertSecurityEvent(ctx, repository.InsertSecurityEventParams{
		OrganizationID: e.OrgID, UserID: &uid, SessionID: e.SessionID,
		EventType: e.Type, Result: e.Result, IpAddress: ip, UserAgent: ua, RequestID: rid, Metadata: meta,
	})
}

// recordFailure mencatat login gagal untuk akun yang DIKENAL (email tak dikenal tidak ditulis:
// tidak ada pemilik yang berhak melihatnya, dan banjir tebakan tidak boleh menjadi banjir tulis).
// Best-effort: kegagalan menulis hanya dilog, tidak mengubah hasil login.
func (s *AuthService) recordFailure(ctx context.Context, userID uuid.UUID, reason string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.insertEvent(wctx, securityEvent{
		Type: EventLoginFailed, Result: eventResultFailure, UserID: userID, Metadata: map[string]any{"reason": reason},
	}); err != nil {
		s.log.WarnContext(ctx, "gagal mencatat event login gagal", "error", err)
	}
}

// record merekam activity atas nama pengguna (di transaksi yang sama bila ctx membawanya).
func (s *AuthService) record(ctx context.Context, userID uuid.UUID, org *uuid.UUID, action string, sessionID uuid.UUID) {
	if s.audit == nil {
		return
	}
	actor := kernel.Actor{UserID: userID, Type: kernel.ActorUser}
	if org != nil {
		actor.OrganizationID = *org
	}
	s.audit.Record(kernel.WithActor(ctx, actor), modulesdk.ActivityEntry{
		Action: action, ResourceType: "session", ResourceID: sessionID,
	})
}

// clientInfo membaca asal request dari ctx untuk kolom ip_address/user_agent.
func clientInfo(ctx context.Context) (*netip.Addr, *string) {
	c, ok := kernel.ClientFromContext(ctx)
	if !ok {
		return nil, nil
	}
	var ip *netip.Addr
	if a, err := netip.ParseAddr(c.IP); err == nil {
		a = a.WithZone("")
		ip = &a
	}
	var ua *string
	if c.UserAgent != "" {
		v := c.UserAgent
		ua = &v
	}
	return ip, ua
}
