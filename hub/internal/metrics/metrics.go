// Package metrics holds what the hub measures, for Prometheus to scrape.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is the hub's own registry and what is registered in it.
type Metrics struct {
	registry *prometheus.Registry

	Requests  *prometheus.CounterVec
	Durations *prometheus.HistogramVec

	Reminders         *prometheus.CounterVec
	Lateness          prometheus.Histogram
	Acknowledged      *prometheus.CounterVec
	Expired           prometheus.Counter
	TickerRuns        *prometheus.CounterVec
	TickerDuration    prometheus.Histogram
	TickerLastSuccess prometheus.Gauge

	open *openDeliveries
}

// New returns a registry that already holds the Go runtime, the process and the build version.
func New(version string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	info := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "plsremind_build_info",
		Help:        "The version of the running hub. Always 1.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	info.Set(1)
	reg.MustRegister(info)
	m := &Metrics{
		registry: reg,
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "plsremind_http_requests_total",
			Help: "HTTP requests by method, route pattern and status.",
		}, []string{"method", "route", "status"}),
		Durations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "plsremind_http_request_duration_seconds",
			Help:    "How long HTTP requests took, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		Reminders: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "plsremind_reminders_total",
			Help: "Reminders whose time came, by kind and by what became of them: fired, superseded or missed.",
		}, []string{"kind", "outcome"}),
		Lateness: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "plsremind_reminder_lateness_seconds",
			Help:    "How long after its time a reminder fired.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 900, 3600},
		}),
		Acknowledged: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "plsremind_deliveries_acknowledged_total",
			Help: "Deliveries a channel confirmed, by result: delivered or failed.",
		}, []string{"result"}),
		Expired: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "plsremind_deliveries_expired_total",
			Help: "Deliveries that nobody confirmed within a day.",
		}),
		TickerRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "plsremind_ticker_runs_total",
			Help: "Rounds of the reminder ticker, by result: ok or error.",
		}, []string{"result"}),
		TickerDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "plsremind_ticker_duration_seconds",
			Help:    "How long a round of the reminder ticker took.",
			Buckets: prometheus.DefBuckets,
		}),
		TickerLastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "plsremind_ticker_last_success_timestamp_seconds",
			Help: "When the reminder ticker last finished a round without an error.",
		}),
	}
	reg.MustRegister(m.Requests, m.Durations, m.Reminders, m.Lateness, m.Acknowledged, m.Expired,
		m.TickerRuns, m.TickerDuration, m.TickerLastSuccess)
	return m
}

// Reminder notes a reminder whose time came. late only counts for one that fired.
func (m *Metrics) Reminder(kind, outcome string, late time.Duration) {
	if m == nil {
		return
	}
	m.Reminders.WithLabelValues(kind, outcome).Inc()
	if outcome == "fired" {
		m.Lateness.Observe(late.Seconds())
	}
}

// Expire notes deliveries that nobody confirmed in time.
func (m *Metrics) Expire(n int) {
	if m != nil {
		m.Expired.Add(float64(n))
	}
}

// Acknowledge notes a delivery a channel confirmed as delivered or failed.
func (m *Metrics) Acknowledge(result string) {
	if m != nil {
		m.Acknowledged.WithLabelValues(result).Inc()
	}
}

// Tick runs one round of the ticker and notes how long it took and how it ended.
func (m *Metrics) Tick(run func() error) error {
	start := time.Now()
	err := run()
	if m == nil {
		return err
	}
	m.TickerDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		m.TickerRuns.WithLabelValues("error").Inc()
		return err
	}
	m.TickerRuns.WithLabelValues("ok").Inc()
	m.TickerLastSuccess.SetToCurrentTime()
	return nil
}

// CountOpenDeliveries reports the deliveries still waiting, asked at each scrape. A later call replaces the count.
func (m *Metrics) CountOpenDeliveries(count func(ctx context.Context) (int64, error)) {
	if m.open == nil {
		m.open = &openDeliveries{
			desc: prometheus.NewDesc("plsremind_deliveries_open", "Deliveries still waiting for a channel to confirm them.", nil, nil),
		}
		m.registry.MustRegister(m.open)
	}
	m.open.mu.Lock()
	defer m.open.mu.Unlock()
	m.open.count = count
}

type openDeliveries struct {
	desc  *prometheus.Desc
	mu    sync.Mutex
	count func(ctx context.Context) (int64, error)
}

func (o *openDeliveries) Describe(ch chan<- *prometheus.Desc) {
	ch <- o.desc
}

func (o *openDeliveries) Collect(ch chan<- prometheus.Metric) {
	o.mu.Lock()
	count := o.count
	o.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := count(ctx)
	// A database that cannot answer leaves the number out rather than holding up the whole scrape.
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(o.desc, prometheus.GaugeValue, float64(n))
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Handler answers a scrape with everything in the registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Start serves /metrics on addr until ctx ends. An address that cannot be bound is an error at once.
func Start(ctx context.Context, addr string, m *Metrics) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics listener stopped", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		stopping, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(stopping)
	}()
	return nil
}
