// Package domain memuat tipe inti autentikasi tanpa ketergantungan pada HTTP, DB, atau Redis
// (docs/05 §9, §19).
package domain

import "github.com/google/uuid"

// AuthLevel adalah tingkat autentikasi sesi; nilainya mengikuti CHECK sessions.auth_level.
type AuthLevel string

const (
	AuthLevelPassword AuthLevel = "password"
	AuthLevelMFA      AuthLevel = "mfa" // Phase 2
)

// Valid melaporkan apakah level dikenal.
func (l AuthLevel) Valid() bool { return l == AuthLevelPassword || l == AuthLevelMFA }

// Identity adalah hasil autentikasi yang diterima handler dari middleware; handler tidak pernah
// membaca cookie sendiri (docs/05 §9).
type Identity struct {
	UserID         uuid.UUID
	OrganizationID *uuid.UUID // nil hanya untuk identitas murni tingkat platform
	SessionID      uuid.UUID
	AuthLevel      AuthLevel
}
