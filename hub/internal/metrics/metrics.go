// Package metrics holds what the hub measures, for Prometheus to scrape.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is the hub's own registry and what is registered in it.
type Metrics struct {
	registry *prometheus.Registry
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
	return &Metrics{registry: reg}
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
