package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Meta adalah bagian "meta" envelope sukses (pagination, allowed_actions, dst.; docs/08 §3.1).
type Meta map[string]any

// Items mengubah slice nil menjadi slice kosong supaya koleksi kosong selalu tampil sebagai [],
// bukan null.
func Items[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

type successBody struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// Responder menulis envelope sukses dan error. Satu instance dipakai bersama oleh semua handler.
type Responder struct {
	log *slog.Logger
}

// NewResponder membuat Responder; log dipakai untuk error yang tak dikenali (nil → slog.Default).
func NewResponder(log *slog.Logger) *Responder {
	if log == nil {
		log = slog.Default()
	}
	return &Responder{log: log}
}

func setJSONHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
}

// JSON menulis {"data":..., "meta":...} dengan status yang diberikan; meta kosong tidak ditulis.
func (rs *Responder) JSON(w http.ResponseWriter, r *http.Request, status int, data any, meta Meta) {
	body, err := json.Marshal(successBody{Data: data, Meta: meta})
	if err != nil {
		rs.Error(w, r, InternalError.Wrap(err))
		return
	}
	setJSONHeaders(w)
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// OK menulis 200 dengan envelope sukses.
func (rs *Responder) OK(w http.ResponseWriter, r *http.Request, data any, meta Meta) {
	rs.JSON(w, r, http.StatusOK, data, meta)
}

// Created menulis 201 dengan envelope sukses.
func (rs *Responder) Created(w http.ResponseWriter, r *http.Request, data any, meta Meta) {
	rs.JSON(w, r, http.StatusCreated, data, meta)
}

// NoContent menulis 204 tanpa body.
func (rs *Responder) NoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// resolve memetakan error apa pun ke *Error. Hanya error yang dikenal yang lolos; sisanya
// internal_error (docs/10 §9). Mengembalikan juga apakah error itu "tak dikenal".
func resolve(err error) (ae *Error, unknown bool) {
	if errors.As(err, &ae) {
		return ae, false
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return PayloadTooLarge.Wrap(err), false
	}
	return InternalError.Wrap(err), true
}

// Error menulis envelope error untuk err. Teks err asli tidak pernah dikirim ke klien; untuk
// kegagalan 5xx yang membawa cause (atau error tak dikenal) detail lengkap masuk log.
func (rs *Responder) Error(w http.ResponseWriter, r *http.Request, err error) {
	ae, unknown := resolve(err)
	if unknown || (ae.Code.Status >= 500 && ae.cause != nil) {
		rs.logFailure(r.Context(), ae, err)
	}

	body := errorBody{
		Code:      ae.Code.Name,
		Message:   ae.PublicMessage(),
		RequestID: logger.RequestID(r.Context()),
	}
	if ae.Code.Name != InternalError.Name {
		body.Details = ae.Details
	} else {
		body.Message = InternalError.Message
	}

	if ae.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(ae.RetryAfter.Seconds()))))
	}
	setJSONHeaders(w)
	w.WriteHeader(ae.Code.Status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: body})
}

func (rs *Responder) logFailure(ctx context.Context, ae *Error, original error) {
	rs.log.ErrorContext(ctx, "unhandled error",
		slog.String("code", ae.Code.Name),
		slog.Int("status", ae.Code.Status),
		slog.String("error", original.Error()),
	)
}
