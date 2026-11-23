// Package metrics holds what the hub measures, for Prometheus to scrape.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	Expired           *prometheus.CounterVec
	TickerRuns        *prometheus.CounterVec
	TickerDuration    prometheus.Histogram
	TickerLastSuccess prometheus.Gauge

	GatewayCloses     *prometheus.CounterVec
	GatewayDispatches *prometheus.CounterVec

	open     *openDeliveries
	sessions *sessions
	pool     *pool
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
		Expired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "plsremind_deliveries_expired_total",
			Help: "Deliveries that nobody confirmed within a day, by the kind of channel: connection or personal_token.",
		}, []string{"channel"}),
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
	m.GatewayCloses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "plsremind_gateway_closes_total",
		Help: "Gateway connections that ended, by close code.",
	}, []string{"code"})
	m.GatewayDispatches = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "plsremind_gateway_dispatches_total",
		Help: "Dispatches produced for gateway sessions, by event.",
	}, []string{"event"})
	reg.MustRegister(m.Requests, m.Durations, m.Reminders, m.Lateness, m.Acknowledged, m.Expired,
		m.TickerRuns, m.TickerDuration, m.TickerLastSuccess, m.GatewayCloses, m.GatewayDispatches)
	for _, channel := range []string{"connection", "personal_token"} {
		m.Expired.WithLabelValues(channel)
	}
	// From the start of the process, so that a hub that just started does not look stalled for decades.
	m.TickerLastSuccess.SetToCurrentTime()
	return m
}

// GatewayClose notes a gateway connection that ended with code.
func (m *Metrics) GatewayClose(code int) {
	if m != nil {
		m.GatewayCloses.WithLabelValues(strconv.Itoa(code)).Inc()
	}
}

// Dispatch notes a dispatch produced for a gateway session.
func (m *Metrics) Dispatch(event string) {
	if m != nil {
		m.GatewayDispatches.WithLabelValues(event).Inc()
	}
}

// CountSessions reports the attached gateway sessions by kind, asked at each scrape. A later call replaces the count.
func (m *Metrics) CountSessions(attached func() map[string]int) {
	if m.sessions == nil {
		m.sessions = &sessions{desc: prometheus.NewDesc("plsremind_gateway_sessions",
			"Gateway sessions with a client attached, by the kind of credential behind them.", []string{"kind"}, nil),
			attached: attached}
		m.registry.MustRegister(m.sessions)
		return
	}
	m.sessions.mu.Lock()
	defer m.sessions.mu.Unlock()
	m.sessions.attached = attached
}

type sessions struct {
	desc     *prometheus.Desc
	mu       sync.Mutex
	attached func() map[string]int
}

func (s *sessions) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.desc
}

func (s *sessions) Collect(ch chan<- prometheus.Metric) {
	s.mu.Lock()
	attached := s.attached
	s.mu.Unlock()
	for kind, n := range attached() {
		ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, float64(n), kind)
	}
}

// WatchPool reports the database pool, asked at each scrape. A later call replaces the pool.
func (m *Metrics) WatchPool(stat func() *pgxpool.Stat) {
	if m.pool == nil {
		m.pool = &pool{
			conns:   prometheus.NewDesc("plsremind_db_pool_connections", "Database connections of the pool, by state.", []string{"state"}, nil),
			acquire: prometheus.NewDesc("plsremind_db_pool_acquire_seconds_total", "Time spent getting a connection from the pool.", nil, nil),
			empty:   prometheus.NewDesc("plsremind_db_pool_empty_acquires_total", "Requests for a connection that had to wait because none was free.", nil, nil),
			stat:    stat,
		}
		m.registry.MustRegister(m.pool)
		return
	}
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()
	m.pool.stat = stat
}

type pool struct {
	conns, acquire, empty *prometheus.Desc
	mu                    sync.Mutex
	stat                  func() *pgxpool.Stat
}

func (p *pool) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.conns
	ch <- p.acquire
	ch <- p.empty
}

func (p *pool) Collect(ch chan<- prometheus.Metric) {
	p.mu.Lock()
	stat := p.stat()
	p.mu.Unlock()
	for state, n := range map[string]int32{
		"acquired": stat.AcquiredConns(), "idle": stat.IdleConns(), "total": stat.TotalConns(), "max": stat.MaxConns(),
	} {
		ch <- prometheus.MustNewConstMetric(p.conns, prometheus.GaugeValue, float64(n), state)
	}
	ch <- prometheus.MustNewConstMetric(p.acquire, prometheus.CounterValue, stat.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(p.empty, prometheus.CounterValue, float64(stat.EmptyAcquireCount()))
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

// Expire notes deliveries on a kind of channel that nobody confirmed in time.
func (m *Metrics) Expire(channel string, n int) {
	if m != nil {
		m.Expired.WithLabelValues(channel).Add(float64(n))
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
			desc:  prometheus.NewDesc("plsremind_deliveries_open", "Deliveries still waiting for a channel to confirm them.", nil, nil),
			count: count,
		}
		m.registry.MustRegister(m.open)
		return
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
	// No number is better than a scrape that hangs on a database that is away.
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
