package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// outboxReadTimeout membatasi pembacaan statistik outbox saat scrape, supaya database yang
// lambat tidak menahan scrape (dan goroutine-nya) tanpa batas.
const outboxReadTimeout = 2 * time.Second

// PoolStat adalah potret satu pool database saat scrape.
type PoolStat struct {
	Acquired int32 // koneksi sedang dipakai
	Idle     int32 // koneksi menganggur
	Max      int32 // batas pool
}

type poolCollector struct {
	stat     func() PoolStat
	acquired *prometheus.Desc
	idle     *prometheus.Desc
	max      *prometheus.Desc
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquired
	ch <- c.idle
	ch <- c.max
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat() // satu potret konsisten untuk ketiga gauge
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(s.Acquired))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(s.Idle))
	ch <- prometheus.MustNewConstMetric(c.max, prometheus.GaugeValue, float64(s.Max))
}

// RegisterPool mendaftarkan gauge db_pool_acquired_connections, db_pool_idle_connections, dan
// db_pool_max_connections dengan label pool=name. stat dipanggil pada setiap scrape.
func (r *Registry) RegisterPool(name string, stat func() PoolStat) error {
	labels := prometheus.Labels{"pool": name}
	c := &poolCollector{
		stat: stat,
		acquired: prometheus.NewDesc("db_pool_acquired_connections",
			"Koneksi pool database yang sedang dipakai.", nil, labels),
		idle: prometheus.NewDesc("db_pool_idle_connections",
			"Koneksi pool database yang menganggur.", nil, labels),
		max: prometheus.NewDesc("db_pool_max_connections",
			"Batas koneksi pool database.", nil, labels),
	}
	if err := r.reg.Register(c); err != nil {
		return fmt.Errorf("daftarkan metrik pool %q: %w", name, err)
	}
	return nil
}

// OutboxStats adalah ringkasan antrian outbox saat scrape.
type OutboxStats struct {
	Pending          int64
	OldestPendingAge time.Duration // 0 bila tak ada yang pending
}

// OutboxReader membaca statistik outbox (dari database). Dipanggil pada setiap scrape dengan
// context ber-batas-waktu.
type OutboxReader func(ctx context.Context) (OutboxStats, error)

type outboxCollector struct {
	read    OutboxReader
	pending *prometheus.Desc
	oldest  *prometheus.Desc
}

func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.oldest
}

func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), outboxReadTimeout)
	defer cancel()
	s, err := c.read(ctx)
	if err != nil {
		// Metrik tak valid: dilewati saat Gather dan dilaporkan lewat ErrorLog (ContinueOnError),
		// sehingga scrape tetap 200 dan metrik lain utuh. Gauge yang hilang itulah sinyalnya.
		ch <- prometheus.NewInvalidMetric(c.pending, err)
		ch <- prometheus.NewInvalidMetric(c.oldest, err)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(s.Pending))
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, s.OldestPendingAge.Seconds())
}

// RegisterOutbox mendaftarkan gauge outbox_events_pending dan outbox_oldest_pending_age_seconds.
// Dibaca dari database saat scrape, jadi tetap terlihat walau worker mati (docs/14 §5: inilah
// sinyal ketiadaan worker). Hanya satu pembaca per registry.
func (r *Registry) RegisterOutbox(read OutboxReader) error {
	c := &outboxCollector{
		read: read,
		pending: prometheus.NewDesc("outbox_events_pending",
			"Jumlah event outbox berstatus pending.", nil, nil),
		oldest: prometheus.NewDesc("outbox_oldest_pending_age_seconds",
			"Umur (detik) event outbox pending tertua; 0 bila tak ada.", nil, nil),
	}
	if err := r.reg.Register(c); err != nil {
		return fmt.Errorf("daftarkan gauge outbox: %w", err)
	}
	return nil
}
