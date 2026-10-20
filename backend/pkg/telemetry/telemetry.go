package telemetry

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// UnmatchedRoute adalah label route untuk request tanpa pola rute (404/405): satu nilai saja
	// agar path acak dari klien tidak menciptakan deret baru.
	UnmatchedRoute = "unmatched"
	// OtherMethod adalah label method untuk metode HTTP di luar himpunan standar.
	OtherMethod = "OTHER"
)

// Registry membungkus satu prometheus.Registry per proses. Selain metrik runtime Go dan proses,
// metrik lain didaftarkan eksplisit oleh composition root (NewHTTP, NewWorker, RegisterPool,
// RegisterOutbox) sehingga API dan worker hanya menampilkan metrik yang memang miliknya.
type Registry struct {
	reg *prometheus.Registry
	log *slog.Logger
}

// NewRegistry membuat registry dengan collector runtime Go dan proses. log menerima kegagalan
// pengumpulan metrik (mis. database mati saat membaca gauge outbox); nil memakai slog.Default.
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return &Registry{reg: reg, log: log}
}

// Handler melayani format eksposisi Prometheus. Kegagalan satu collector TIDAK menggagalkan
// scrape: metrik lain tetap keluar dan penyebabnya dilog (bukan dikirim ke klien).
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		ErrorLog:      errorLog{log: r.log},
		ErrorHandling: promhttp.ContinueOnError,
	})
}

type errorLog struct{ log *slog.Logger }

func (e errorLog) Println(v ...any) {
	e.log.Error("telemetry: gagal mengumpulkan metrik", "detail", fmt.Sprint(v...))
}

// HTTP adalah metrik request HTTP (docs/14 §5).
type HTTP struct {
	duration *prometheus.HistogramVec
	total    *prometheus.CounterVec
}

// NewHTTP mendaftarkan http_request_duration_seconds dan http_requests_total.
func (r *Registry) NewHTTP() (*HTTP, error) {
	labels := []string{"route", "method", "status"}
	h := &HTTP{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Durasi pemrosesan request HTTP (route = pola rute, bukan path mentah).",
			Buckets: prometheus.DefBuckets,
		}, labels),
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Jumlah request HTTP (route = pola rute, bukan path mentah).",
		}, labels),
	}
	if err := r.reg.Register(h.duration); err != nil {
		return nil, fmt.Errorf("daftarkan http_request_duration_seconds: %w", err)
	}
	if err := r.reg.Register(h.total); err != nil {
		return nil, fmt.Errorf("daftarkan http_requests_total: %w", err)
	}
	return h, nil
}

// ObserveRequest mencatat satu request. route HARUS pola rute (mis. "/api/v1/items/{id}"); string
// kosong (tak ada rute cocok) menjadi UnmatchedRoute. Metode di luar himpunan standar menjadi
// OtherMethod karena klien bebas mengirim metode apa pun.
func (h *HTTP) ObserveRequest(route, method string, status int, d time.Duration) {
	if route == "" {
		route = UnmatchedRoute
	}
	lv := []string{route, normalizeMethod(method), strconv.Itoa(status)}
	h.total.WithLabelValues(lv...).Inc()
	h.duration.WithLabelValues(lv...).Observe(d.Seconds())
}

func normalizeMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return m
	default:
		return OtherMethod
	}
}

// Worker adalah metrik proses worker (docs/14 §5).
type Worker struct {
	duration *prometheus.HistogramVec
	failures *prometheus.CounterVec
	dead     prometheus.Counter
}

// NewWorker mendaftarkan worker_job_duration_seconds, worker_job_failures_total, dan
// outbox_events_dead_total. Hanya dipanggil di proses worker.
func (r *Registry) NewWorker() (*Worker, error) {
	w := &Worker{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "worker_job_duration_seconds",
			Help:    "Durasi satu job worker per jenis job.",
			Buckets: prometheus.DefBuckets,
		}, []string{"job_type"}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worker_job_failures_total",
			Help: "Jumlah job worker yang gagal per jenis job.",
		}, []string{"job_type"}),
		dead: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_events_dead_total",
			Help: "Jumlah event outbox yang menjadi dead-letter sejak proses worker start.",
		}),
	}
	for _, c := range []prometheus.Collector{w.duration, w.failures, w.dead} {
		if err := r.reg.Register(c); err != nil {
			return nil, fmt.Errorf("daftarkan metrik worker: %w", err)
		}
	}
	return w, nil
}

// ObserveJob mencatat satu job selesai; err != nil menaikkan worker_job_failures_total.
// jobType harus berasal dari kode (himpunan terbatas), bukan dari input.
func (w *Worker) ObserveJob(jobType string, d time.Duration, err error) {
	w.duration.WithLabelValues(jobType).Observe(d.Seconds())
	if err != nil {
		w.failures.WithLabelValues(jobType).Inc()
	}
}

// OutboxDead menaikkan outbox_events_dead_total (dipanggil dari hook dead-letter relay).
func (w *Worker) OutboxDead() { w.dead.Inc() }
