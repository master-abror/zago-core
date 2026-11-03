package authzhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Kode error Authorization Engine (docs/06 §12, docs/08 §11). permission_denied milik kernel
// (httpx.PermissionDenied); kode lain di daftar docs/06 §12 didaftarkan oleh task yang memakainya.
var (
	// ScopeMismatch: sumber daya atau scope request berada di luar organisasi sesi (docs/06 §2,
	// §5.6 butir 6). Status 403 seperti permission_denied, tetapi kodenya sendiri supaya UI dan
	// pencarian audit dapat membedakannya.
	ScopeMismatch = httpx.Register(httpx.Code{Name: "scope_mismatch", Status: http.StatusForbidden, Message: "The resource is outside the scope of this request."})

	// PolicyEvaluationFailed: sebuah policy tidak dapat dievaluasi sehingga request ditolak
	// gagal-tertutup (ADR-0022 butir 31). 403 (penolakan, bukan galat server) dengan kode sendiri
	// supaya admin dapat membedakan policy rusak dari penolakan biasa; pesan tidak menyebut policy.
	PolicyEvaluationFailed = httpx.Register(httpx.Code{Name: "policy_evaluation_failed", Status: http.StatusForbidden, Message: "The request could not be authorized."})
)
