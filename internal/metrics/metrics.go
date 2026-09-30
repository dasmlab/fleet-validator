// Package metrics exports validation reports as Prometheus gauges.
//
// The validated cluster is labelled managed_cluster, not cluster: ACM Observability stamps
// its own cluster label (the hub, local-cluster) on everything it forwards to Thanos.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dasmlab/fleet-validator/internal/checks"
)

const (
	ns = "fleetvalidator"

	lCluster  = "managed_cluster"
	lType     = "cluster_type"
	lGroup    = "group"
	lCheck    = "check"
	lTitle    = "title"
	lSeverity = "severity"
	lState    = "state"
	lDetail   = "detail"

	maxDetail = 180
)

// Names lists every exported series name (for the MCO allowlist).
var Names = []string{
	ns + "_check_state", ns + "_check_detail", ns + "_cluster_score", ns + "_group_score",
	ns + "_cluster_ready", ns + "_cluster_checks", ns + "_cluster_info",
	ns + "_last_validation_timestamp_seconds", ns + "_validation_duration_seconds",
	ns + "_clusters", ns + "_build_info",
}

// Exporter holds the gauges.
type Exporter struct {
	mu sync.Mutex

	checkState  *prometheus.GaugeVec
	checkDetail *prometheus.GaugeVec
	score       *prometheus.GaugeVec
	groupScore  *prometheus.GaugeVec
	ready       *prometheus.GaugeVec
	counts      *prometheus.GaugeVec
	info        *prometheus.GaugeVec
	last        *prometheus.GaugeVec
	duration    *prometheus.GaugeVec
	clusters    *prometheus.GaugeVec
}

// New registers the gauges on reg.
func New(reg prometheus.Registerer, version string) *Exporter {
	g := func(name, help string, labels ...string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: ns, Name: name, Help: help}, labels)
	}
	e := &Exporter{
		checkState: g("check_state", "Check result: 1 pass, 0.5 warn, 0 fail, -1 skip.",
			lCluster, lType, lGroup, lCheck, lTitle, lSeverity),
		checkDetail: g("check_detail", "Always 1; the detail label carries the last check message.",
			lCluster, lType, lGroup, lCheck, lDetail),
		score:      g("cluster_score", "Weighted readiness score 0-100.", lCluster, lType),
		groupScore: g("group_score", "Weighted readiness score 0-100 per checklist group.", lCluster, lType, lGroup),
		ready:      g("cluster_ready", "1 when no critical check fails.", lCluster, lType),
		counts:     g("cluster_checks", "Number of checks per state.", lCluster, lType, lState),
		info: g("cluster_info", "Cluster facts as labels.",
			lCluster, lType, "openshift_version", "acm_version", "platform", "vendor"),
		last:     g("last_validation_timestamp_seconds", "Unix time of the last validation.", lCluster, lType),
		duration: g("validation_duration_seconds", "Duration of the last validation.", lCluster, lType),
		clusters: g("clusters", "Clusters being validated.", lType),
	}
	build := g("build_info", "Build version.", "version")
	build.WithLabelValues(version).Set(1)
	reg.MustRegister(e.checkState, e.checkDetail, e.score, e.groupScore, e.ready, e.counts,
		e.info, e.last, e.duration, e.clusters, build)
	return e
}

// Set replaces every series of r.Cluster with r.
func (e *Exporter) Set(r checks.Report) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deleteLocked(r.Cluster)
	typ := string(r.Type)
	for _, res := range r.Results {
		e.checkState.WithLabelValues(r.Cluster, typ, res.Group, res.ID, res.Title, string(res.Severity)).Set(res.State.Value())
		e.checkDetail.WithLabelValues(r.Cluster, typ, res.Group, res.ID, truncate(res.Detail)).Set(1)
	}
	e.score.WithLabelValues(r.Cluster, typ).Set(r.Score)
	for grp, s := range r.GroupScores {
		e.groupScore.WithLabelValues(r.Cluster, typ, grp).Set(s)
	}
	ready := 0.0
	if r.Ready {
		ready = 1
	}
	e.ready.WithLabelValues(r.Cluster, typ).Set(ready)
	for _, st := range []checks.State{checks.Pass, checks.Warn, checks.Fail, checks.Skip} {
		e.counts.WithLabelValues(r.Cluster, typ, string(st)).Set(float64(r.Counts[st]))
	}
	e.info.WithLabelValues(r.Cluster, typ, r.Info["openshift_version"], r.Info["acm_version"],
		r.Info["platform"], r.Info["vendor"]).Set(1)
	e.last.WithLabelValues(r.Cluster, typ).Set(float64(r.CheckedAt.Unix()))
	e.duration.WithLabelValues(r.Cluster, typ).Set(r.Duration)
}

// Delete drops a cluster that left the fleet.
func (e *Exporter) Delete(cluster string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deleteLocked(cluster)
}

func (e *Exporter) deleteLocked(cluster string) {
	m := prometheus.Labels{lCluster: cluster}
	for _, v := range []*prometheus.GaugeVec{e.checkState, e.checkDetail, e.score, e.groupScore,
		e.ready, e.counts, e.info, e.last, e.duration} {
		v.DeletePartialMatch(m)
	}
}

// SetClusterCounts updates the number of clusters per type.
func (e *Exporter) SetClusterCounts(hub, managed int) {
	e.clusters.WithLabelValues(string(checks.Hub)).Set(float64(hub))
	e.clusters.WithLabelValues(string(checks.Managed)).Set(float64(managed))
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) <= maxDetail {
		return s
	}
	return string(r[:maxDetail-1]) + "…"
}
