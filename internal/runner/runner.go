// Package runner discovers the hub and its managed clusters and validates each one every
// interval, a few at a time, with starts spread over the interval so the hub API is not hit
// by every cluster at once.
package runner

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/dasmlab/fleet-validator/internal/checks"
	"github.com/dasmlab/fleet-validator/internal/config"
	"github.com/dasmlab/fleet-validator/internal/metrics"
)

var gvrManagedCluster = schema.GroupVersionResource{
	Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters",
}

// Runner owns the latest report per cluster.
type Runner struct {
	Env     *checks.Env
	Metrics *metrics.Exporter
	Log     logr.Logger

	mu       sync.RWMutex
	reports  map[string]checks.Report
	inFlight map[string]bool
	ready    bool
}

// New builds a Runner.
func New(env *checks.Env, m *metrics.Exporter, log logr.Logger) *Runner {
	return &Runner{Env: env, Metrics: m, Log: log, reports: map[string]checks.Report{}, inFlight: map[string]bool{}}
}

type target struct {
	name string
	typ  checks.ClusterType
	mc   *unstructured.Unstructured
}

// Run loops until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	interval := r.Env.Cfg.IntervalDuration()
	r.cycle(ctx, interval)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.cycle(ctx, interval)
		}
	}
}

// Once validates every cluster one time, synchronously (used by --once).
func (r *Runner) Once(ctx context.Context) []checks.Report {
	targets, err := r.discover(ctx)
	if err != nil {
		r.Log.Error(err, "discovery failed")
	}
	for _, t := range targets {
		r.validate(ctx, t)
	}
	return r.Reports()
}

func (r *Runner) cycle(ctx context.Context, interval time.Duration) {
	targets, err := r.discover(ctx)
	if err != nil {
		// Validate the hub anyway, but keep the managed clusters' last results.
		r.Log.Error(err, "ManagedCluster discovery failed; validating the hub only")
	} else {
		r.prune(targets)
	}

	sem := make(chan struct{}, r.Env.Cfg.Concurrency)
	// Spread starts over half the interval so a big fleet does not burst the hub API.
	step := interval / 2 / time.Duration(len(targets)+1)
	for i, t := range targets {
		if !r.claim(t.name) {
			r.Log.V(1).Info("previous validation still running; skipping", "cluster", t.name)
			continue
		}
		go func(delay time.Duration, t target) {
			defer r.release(t.name)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			r.validate(ctx, t)
		}(time.Duration(i)*step, t)
	}
}

func (r *Runner) validate(ctx context.Context, t target) {
	list := checks.ManagedChecks()
	if t.typ == checks.Hub {
		list = checks.HubChecks()
	}
	rep := checks.Validate(ctx, checks.NewTarget(t.name, t.typ, t.mc, r.Env), list)
	r.Metrics.Set(rep)
	r.mu.Lock()
	r.reports[t.name] = rep
	r.ready = true
	r.mu.Unlock()
	r.Log.Info("validated", "cluster", t.name, "type", t.typ, "score", rep.Score, "ready", rep.Ready,
		"fail", rep.Counts[checks.Fail], "warn", rep.Counts[checks.Warn], "seconds", rep.Duration)
}

// discover returns the hub plus every ManagedCluster that is at least accepted and joined.
func (r *Runner) discover(ctx context.Context) ([]target, error) {
	list, err := r.Env.Dyn.Resource(gvrManagedCluster).List(ctx, metav1.ListOptions{})
	if err != nil {
		// No ACM API: still validate the hub platform.
		r.Metrics.SetClusterCounts(1, 0)
		return []target{{name: r.Env.Cfg.HubName, typ: checks.Hub}}, err
	}
	var hub *target
	var managed []target
	for i := range list.Items {
		mc := &list.Items[i]
		if mc.GetAnnotations()[config.AnnotationSkip] == "true" {
			continue
		}
		if mc.GetLabels()["local-cluster"] == "true" {
			hub = &target{name: mc.GetName(), typ: checks.Hub, mc: mc}
			continue
		}
		if joined(mc) {
			managed = append(managed, target{name: mc.GetName(), typ: checks.Managed, mc: mc})
		}
	}
	if hub == nil {
		hub = &target{name: r.Env.Cfg.HubName, typ: checks.Hub}
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i].name < managed[j].name })
	r.Metrics.SetClusterCounts(1, len(managed))
	return append([]target{*hub}, managed...), nil
}

func joined(mc *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(mc.Object, "status", "conditions")
	accepted, joinedOK := false, false
	for _, raw := range conds {
		c, ok := raw.(map[string]any)
		if !ok || c["status"] != "True" {
			continue
		}
		switch c["type"] {
		case "HubAcceptedManagedCluster":
			accepted = true
		case "ManagedClusterJoined":
			joinedOK = true
		}
	}
	return accepted && joinedOK
}

func (r *Runner) prune(targets []target) {
	keep := map[string]bool{}
	for _, t := range targets {
		keep[t.name] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.reports {
		if !keep[name] {
			delete(r.reports, name)
			r.Metrics.Delete(name)
			r.Log.Info("cluster left the fleet; dropped", "cluster", name)
		}
	}
}

func (r *Runner) claim(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight[name] {
		return false
	}
	r.inFlight[name] = true
	return true
}

func (r *Runner) release(name string) {
	r.mu.Lock()
	delete(r.inFlight, name)
	r.mu.Unlock()
}

// Reports returns the latest reports, hub first then by name.
func (r *Runner) Reports() []checks.Report {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]checks.Report, 0, len(r.reports))
	for _, rep := range r.reports {
		out = append(out, rep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == checks.Hub
		}
		return out[i].Cluster < out[j].Cluster
	})
	return out
}

// Report returns one cluster's latest report.
func (r *Runner) Report(name string) (checks.Report, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rep, ok := r.reports[name]
	return rep, ok
}

// Ready is true once at least one cluster has been validated.
func (r *Runner) Ready() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready
}
