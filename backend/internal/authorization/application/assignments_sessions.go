package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// SessionRevoker mencabut semua sesi aktif satu user. Didefinisikan di sisi pemakai (paket ini tidak
// mengimpor auth/service); *service.SessionService memenuhinya langsung. Dipanggil SETELAH commit
// pencabutan assignment platform (ADR-0022 butir 76): sesi yang dibuka lewat switch-organization
// membawa Identity.OrganizationID dan tidak diperiksa ulang haknya per request, jadi tanpa ini sesi
// itu tetap hidup walau assignment platform pemiliknya sudah dicabut.
type SessionRevoker interface {
	RevokeAll(ctx context.Context, userID uuid.UUID, reason string) (int, error)
}

// SessionRevokeReason adalah alasan yang dibawa event session.revoked. Nilainya sama dengan
// auth/service.ReasonRevokeAll (dijaga tes perakitan di internal/app).
const SessionRevokeReason = "revoke_all"

// AuditActionSessionsRevoke adalah activity hasil pencabutan sesi sesudah assignment platform
// dicabut; ditulis di luar transaksi pencabutan (result success atau failed).
const AuditActionSessionsRevoke = "role_assignment.sessions_revoke"

// sessionRevokeTimeout membatasi pencabutan sesi sesudah commit. Context dilepas dari pembatalan
// request: assignment sudah dicabut, jadi klien yang memutus koneksi tidak boleh membatalkannya.
const sessionRevokeTimeout = 10 * time.Second

// WithSessionRevoker mengembalikan salinan writer yang mencabut semua sesi user sesudah assignment
// platform miliknya dicabut. log nil memakai slog.Default. Tanpa ini (revoker nil) tidak ada sesi
// yang dicabut; perakitan produksi WAJIB memasangnya (internal/app menolaknya saat start).
func (w *AssignmentWriter) WithSessionRevoker(r SessionRevoker, log *slog.Logger) *AssignmentWriter {
	cp := *w
	cp.sessions = r
	if log == nil {
		log = slog.Default()
	}
	cp.log = log
	return &cp
}

// platformUserSubject mengembalikan id user pemegang assignment platform yang baru dicabut. Hanya
// assignment berscope platform dengan subjek user (butir 47: subjek platform hanya user) yang
// mengubah apa pun pada sesi.
func platformUserSubject(rec domain.AssignmentRecord) (uuid.UUID, bool) {
	if rec.Scope.Level != domain.LevelPlatform || rec.SubjectType != domain.SubjectUser {
		return uuid.Nil, false
	}
	return rec.SubjectID, true
}

// revokeSessions dipanggil SETELAH commit. Kegagalan TIDAK membatalkan pencabutan assignment
// (sudah tercatat dan cache sudah dibatalkan); ia di-log dan diaudit result=failed, dan sesi yang
// tersisa berakhir menurut timeout idle atau absolut. Satu percobaan: tidak ada retry diam-diam.
func (w *AssignmentWriter) revokeSessions(ctx context.Context, g Grantor, org uuid.UUID, assignmentID, subject uuid.UUID) {
	if w.sessions == nil {
		return
	}
	base := context.WithoutCancel(ctx)
	cc, cancel := context.WithTimeout(base, sessionRevokeTimeout)
	defer cancel()

	n, err := w.sessions.RevokeAll(cc, subject, SessionRevokeReason)
	entry := modulesdk.ActivityEntry{
		Action:       AuditActionSessionsRevoke,
		ResourceType: "role_assignment",
		ResourceID:   assignmentID,
		Metadata:     map[string]any{"subject_id": subject.String()},
	}
	if err != nil {
		w.log.ErrorContext(base, "gagal mencabut sesi setelah assignment platform dicabut; sesi tersisa berakhir menurut timeout",
			"assignment_id", assignmentID.String(), "subject_id", subject.String(), "error", err)
		entry.Result = modulesdk.ResultFailed
		entry.Metadata["reason"] = "session_revoke_failed"
		w.audit.Record(actorContext(base, g, org), entry)
		return
	}
	entry.Metadata["sessions"] = n
	w.audit.Record(actorContext(base, g, org), entry)
}
