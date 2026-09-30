// Package httpserver serves /metrics and health on the public port, and the read-only
// checklist UI + JSON API on a separate listener meant to sit behind oauth-proxy.
package httpserver

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dasmlab/fleet-validator/internal/checks"
	"github.com/dasmlab/fleet-validator/internal/runner"
)

//go:embed static
var static embed.FS

// Server wires handlers to the runner.
type Server struct {
	Runner  *runner.Runner
	Reg     *prometheus.Registry
	Version string
}

// MetricsMux serves /metrics, /healthz and /readyz.
func (s *Server) MetricsMux() *http.ServeMux {
	m := http.NewServeMux()
	m.Handle("GET /metrics", promhttp.HandlerFor(s.Reg, promhttp.HandlerOpts{}))
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	m.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.Runner.Ready() {
			http.Error(w, "first validation not finished", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return m
}

// UIMux serves the checklist page and the JSON API.
func (s *Server) UIMux() *http.ServeMux {
	m := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	m.Handle("GET /", http.FileServerFS(sub))
	m.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"version": s.Version})
	})
	m.HandleFunc("GET /api/v1/clusters", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.Runner.Reports())
	})
	m.HandleFunc("GET /api/v1/clusters/{name}", func(w http.ResponseWriter, r *http.Request) {
		rep, ok := s.Runner.Report(r.PathValue("name"))
		if !ok {
			http.Error(w, "unknown cluster", http.StatusNotFound)
			return
		}
		writeJSON(w, rep)
	})
	m.HandleFunc("GET /api/v1/checks", func(w http.ResponseWriter, _ *http.Request) {
		type item struct {
			ID, Group, Title, Command string
			Severity                  checks.Severity
		}
		out := map[checks.ClusterType][]item{}
		for typ, list := range checks.Catalog() {
			for _, c := range list {
				out[typ] = append(out[typ], item{c.ID, c.Group, c.Title, c.Command, c.Severity})
			}
		}
		writeJSON(w, out)
	})
	return m
}

// AllMux serves everything on one listener (local runs without a proxy).
func (s *Server) AllMux() *http.ServeMux {
	m := s.UIMux()
	metrics := s.MetricsMux()
	for _, p := range []string{"GET /metrics", "GET /healthz", "GET /readyz"} {
		m.Handle(p, metrics)
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
