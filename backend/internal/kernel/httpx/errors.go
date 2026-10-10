// Package httpx adalah toolkit HTTP milik kernel (docs/08, docs/10 §9): registry kode error,
// AppError, envelope {data,meta}/{error}, mapper error → respons, dan decode body JSON.
//
// Lapisan domain/aplikasi mengembalikan error Go biasa (sentinel atau dibungkus); hanya lapisan
// transport yang memetakannya ke kode API lewat Responder.Error. Error yang tidak dikenali selalu
// menjadi internal_error tanpa teks aslinya (docs/10 §9).
package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Code adalah satu entri registry error: nama stabil (yang dipakai klien untuk bercabang), status
// HTTP, dan pesan bawaan berbahasa Inggris (docs/08 §3.2, §8).
type Code struct {
	Name    string
	Status  int
	Message string
}

// codeNamePattern: "snake_case" untuk kode platform, atau "modul.snake_case" untuk kode modul
// (docs/08 §11: kode modul diawali kode modulnya supaya tak pernah bertabrakan).
var codeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$`)

var (
	registryMu sync.RWMutex
	registry   = map[string]Code{}
)

// Register mendaftarkan kode ke registry global dan mengembalikannya. Mendaftarkan ulang kode yang
// persis sama diperbolehkan; nama yang sama dengan definisi berbeda, nama tak valid, atau status di
// luar 4xx/5xx adalah kesalahan pemrograman dan membuat panic saat init.
func Register(c Code) Code {
	if !codeNamePattern.MatchString(c.Name) {
		panic(fmt.Sprintf("httpx: nama kode error %q tidak valid", c.Name))
	}
	if c.Status < 400 || c.Status > 599 {
		panic(fmt.Sprintf("httpx: status kode error %q harus 4xx/5xx, dapat %d", c.Name, c.Status))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if prev, ok := registry[c.Name]; ok && prev != c {
		panic(fmt.Sprintf("httpx: kode error %q sudah terdaftar dengan definisi berbeda", c.Name))
	}
	registry[c.Name] = c
	return c
}

// Lookup mengembalikan kode terdaftar menurut namanya.
func Lookup(name string) (Code, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[name]
	return c, ok
}

// Codes mengembalikan seluruh kode terdaftar, terurut menurut nama.
func Codes() []Code {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Code, 0, len(registry))
	for _, c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Kode umum milik kernel (docs/08 §11 "General" + I-08, ditambah authentication_required,
// permission_denied, rate_limit_exceeded yang pemetaan HTTP-nya ditetapkan eksplisit di sana).
// Kode domain lain (auth, authorization, modul, realtime, audit) didaftarkan oleh milestone yang
// memilikinya lewat Register. Lihat ADR-0012 untuk method_not_allowed.
var (
	AuthenticationRequired = Register(Code{"authentication_required", http.StatusUnauthorized, "Authentication is required."})
	PermissionDenied       = Register(Code{"permission_denied", http.StatusForbidden, "You do not have permission to perform this action."})
	ValidationFailed       = Register(Code{"validation_failed", http.StatusBadRequest, "The request is invalid."})
	ResourceNotFound       = Register(Code{"resource_not_found", http.StatusNotFound, "The requested resource was not found."})
	MethodNotAllowed       = Register(Code{"method_not_allowed", http.StatusMethodNotAllowed, "The method is not allowed for this resource."})
	VersionConflict        = Register(Code{"version_conflict", http.StatusConflict, "The resource was modified by someone else."})
	IdempotencyInProgress  = Register(Code{"idempotency_in_progress", http.StatusConflict, "A request with this idempotency key is still being processed."})
	PayloadTooLarge        = Register(Code{"payload_too_large", http.StatusRequestEntityTooLarge, "The request body is too large."})
	UnsupportedMediaType   = Register(Code{"unsupported_media_type", http.StatusUnsupportedMediaType, "The content type is not supported."})
	IdempotencyKeyConflict = Register(Code{"idempotency_key_conflict", http.StatusUnprocessableEntity, "This idempotency key was already used with a different request."})
	RateLimitExceeded      = Register(Code{"rate_limit_exceeded", http.StatusTooManyRequests, "Too many requests."})
	InternalError          = Register(Code{"internal_error", http.StatusInternalServerError, "An internal error occurred."})
	ServiceUnavailable     = Register(Code{"service_unavailable", http.StatusServiceUnavailable, "The service is temporarily unavailable."})
)

// Error (AppError) adalah error yang tahu cara dirender ke klien. Message dan Details boleh
// dilihat klien; cause TIDAK pernah ditulis ke respons, hanya ke log.
type Error struct {
	Code       Code
	Message    string         // menimpa Code.Message bila tidak kosong
	Details    map[string]any // machine-readable, mis. galat per field
	RetryAfter time.Duration  // > 0 → header Retry-After (dibulatkan ke atas, detik)
	cause      error
}

// New membuat Error dengan kode ini.
func (c Code) New() *Error { return &Error{Code: c} }

// Wrap membuat Error dengan kode ini yang membawa cause (hanya untuk log dan errors.Is/As).
func (c Code) Wrap(cause error) *Error { return &Error{Code: c, cause: cause} }

// WithMessage menimpa pesan bawaan (tetap bahasa Inggris; klien memetakan `code`, bukan pesan).
func (e *Error) WithMessage(msg string) *Error { e.Message = msg; return e }

// WithDetails mengisi details machine-readable.
func (e *Error) WithDetails(d map[string]any) *Error { e.Details = d; return e }

// WithRetryAfter mengisi header Retry-After (untuk 429/503).
func (e *Error) WithRetryAfter(d time.Duration) *Error { e.RetryAfter = d; return e }

// Error mengimplementasikan error. Hanya untuk log, bukan untuk klien.
func (e *Error) Error() string {
	var sb strings.Builder
	sb.WriteString(e.Code.Name)
	if e.Message != "" {
		sb.WriteString(": ")
		sb.WriteString(e.Message)
	}
	if e.cause != nil {
		sb.WriteString(": ")
		sb.WriteString(e.cause.Error())
	}
	return sb.String()
}

// Unwrap mengekspos cause untuk errors.Is/As.
func (e *Error) Unwrap() error { return e.cause }

// PublicMessage adalah pesan yang boleh dilihat klien.
func (e *Error) PublicMessage() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code.Message
}

// HasCode melaporkan apakah err (atau error yang dibungkusnya) adalah *Error dengan kode c.
func HasCode(err error, c Code) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Code.Name == c.Name
}
