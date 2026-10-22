// Package authhttp adalah lapisan transport HTTP autentikasi (docs/05 §8–§9, §17): cookie
// sesi, kebijakan Origin, CSRF, dan middleware Authenticate/CSRF/RequireAuth. Handler endpoint
// ada di paket ini juga; handler tidak pernah membaca cookie sendiri, hanya Identity.
package authhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Kode error autentikasi (docs/05 §18, docs/08 §11). Kode lain di daftar itu didaftarkan oleh
// task yang memakainya.
var (
	// CSRFFailed: token CSRF hilang/salah/milik sesi lain, atau Origin tidak diizinkan.
	CSRFFailed = httpx.Register(httpx.Code{Name: "csrf_failed", Status: http.StatusForbidden, Message: "CSRF validation failed."})
)
