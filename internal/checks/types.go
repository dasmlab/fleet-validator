// Package checks holds the readiness checklist: hub checks (run against local-cluster with the
// pod's own ServiceAccount) and managed-cluster checks (read from the hub's view of each spoke).
package checks

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/fleet-validator/internal/config"
	"github.com/dasmlab/fleet-validator/internal/promq"
)

// Severity weights a check in the cluster score.
type Severity string

const (
	Critical Severity = "critical"
	Warning  Severity = "warning"
	Info     Severity = "info"
)

// Weight is the score weight of a severity.
func (s Severity) Weight() float64 {
	switch s {
	case Critical:
		return 5
	case Warning:
		return 2
	default:
		return 1
	}
}

// State is the outcome of one check.
type State string

const (
	Pass State = "pass"
	Warn State = "warn"
	Fail State = "fail"
	// Skip means not applicable or no data (CRD absent, probe policy not placed); excluded from the score.
	Skip State = "skip"
)

// Value is the Prometheus encoding of a state.
func (s State) Value() float64 {
	switch s {
	case Pass:
		return 1
	case Warn:
		return 0.5
	case Fail:
		return 0
	default:
		return -1
	}
}

// ClusterType separates the two checklists (and the two Grafana dashboards).
type ClusterType string

const (
	Hub     ClusterType = "hub"
	Managed ClusterType = "managed"
)

// Check is one checklist item.
type Check struct {
	ID       string
	Group    string
	Title    string
	Severity Severity
	// Command is the oc equivalent, shown in the UI and docs.
	Command string
	Run     func(ctx context.Context, t *Target) (State, string)
}

// Result is the outcome of one check on one cluster.
type Result struct {
	ID       string   `json:"id"`
	Group    string   `json:"group"`
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	State    State    `json:"state"`
	Detail   string   `json:"detail,omitempty"`
	Command  string   `json:"command,omitempty"`
}

// Env is shared by all checks.
type Env struct {
	Dyn  dynamic.Interface
	Kube kubernetes.Interface
	// Prom queries the hub's in-cluster Thanos querier; nil disables metric-based checks.
	Prom *promq.Client
	Cfg  *config.Config
}

// Target is one cluster being validated. A Target lives for one validation run, so its
// memo cache never serves data older than the run.
type Target struct {
	Name string
	Type ClusterType
	// ManagedCluster is the hub's ManagedCluster object; nil only for a hub without local-cluster.
	ManagedCluster *unstructured.Unstructured
	Env            *Env

	mu   sync.Mutex
	memo map[string]memoEntry
}

type memoEntry struct {
	val any
	err error
}

// NewTarget builds a Target for one run.
func NewTarget(name string, typ ClusterType, mc *unstructured.Unstructured, env *Env) *Target {
	return &Target{Name: name, Type: typ, ManagedCluster: mc, Env: env, memo: map[string]memoEntry{}}
}

// once memoizes fn under key for the lifetime of the Target.
func (t *Target) once(key string, fn func() (any, error)) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.memo[key]; ok {
		return e.val, e.err
	}
	v, err := fn()
	t.memo[key] = memoEntry{val: v, err: err}
	return v, err
}

// Governance returns the policy thresholds for this cluster (config, then ManagedCluster annotations).
func (t *Target) Governance() config.GovernanceThresholds {
	var ann map[string]string
	if t.ManagedCluster != nil {
		ann = t.ManagedCluster.GetAnnotations()
	}
	return t.Env.Cfg.GovernanceFor(t.Name, ann)
}
