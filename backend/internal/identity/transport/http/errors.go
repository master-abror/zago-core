// Package identityhttp memuat lapisan HTTP identitas (docs/08 §10.8-§10.9, ADR-0023 butir
// 21-26): kode error, pemetaan galat domain ke kode API, dan DTO profil dan user. Handler
// berada di paket yang sama.
package identityhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// UserNotFound: user tidak ada, sudah dihapus, atau bukan milik organisasi pemanggil. Ketiga
// keadaan memberi respons yang sama, supaya kode ini bukan alat menebak id atau keanggotaan lintas
// tenant (ADR-0023 butir 21b). Kode umum (validation_failed, permission_denied, internal_error)
// dipakai untuk galat masukan, operasi platform-saja, dan galat pemrograman.
var UserNotFound = httpx.Register(httpx.Code{Name: "user_not_found", Status: http.StatusNotFound, Message: "The user was not found."})
