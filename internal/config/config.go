// Package config loads the fleet-validator settings (ConfigMap-mounted YAML, all optional).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Annotations on a ManagedCluster that override the governance thresholds for that cluster.
const (
	AnnotationMinOperatorPolicies      = "fleet-validator.dasmlab.org/min-operator-policies"
	AnnotationMinConfigurationPolicies = "fleet-validator.dasmlab.org/min-configuration-policies"
	// AnnotationSkip excludes a ManagedCluster from validation.
	AnnotationSkip = "fleet-validator.dasmlab.org/skip"
)

// GovernanceThresholds is what a cluster must have bound to it.
type GovernanceThresholds struct {
	MinOperatorPolicies      int `json:"minOperatorPolicies"`
	MinConfigurationPolicies int `json:"minConfigurationPolicies"`
}

// Governance holds the default thresholds and per-cluster overrides.
type Governance struct {
	Default  GovernanceThresholds            `json:"default"`
	Clusters map[string]GovernanceThresholds `json:"clusters,omitempty"`
}

// Thresholds are numeric limits used by individual checks.
type Thresholds struct {
	APIP99Seconds          float64 `json:"apiP99Seconds"`
	EtcdDBWarnBytes        int64   `json:"etcdDBWarnBytes"`
	EtcdDBFailBytes        int64   `json:"etcdDBFailBytes"`
	LeaseStaleSeconds      int     `json:"leaseStaleSeconds"`
	CSRPendingGraceMinutes int     `json:"csrPendingGraceMinutes"`
	BackupMaxAgeHours      int     `json:"backupMaxAgeHours"`
}

// Config is the whole file.
type Config struct {
	// Interval between validations of the same cluster.
	Interval string `json:"interval"`
	// Concurrency is how many clusters are validated at the same time.
	Concurrency int `json:"concurrency"`
	// HubName is used when the hub has no local-cluster ManagedCluster.
	HubName                string `json:"hubName"`
	HubNamespace           string `json:"hubNamespace"`
	BackupNamespace        string `json:"backupNamespace"`
	ObservabilityNamespace string `json:"observabilityNamespace"`
	// ProbePolicy is the root policy that runs the spoke probes: a name (any namespace) or
	// namespace.name.
	ProbePolicy    string     `json:"probePolicy"`
	RequiredAddons []string   `json:"requiredAddons"`
	Governance     Governance `json:"governance"`
	Thresholds     Thresholds `json:"thresholds"`
	DisabledChecks []string   `json:"disabledChecks,omitempty"`
	PrometheusURL  string     `json:"prometheusURL"`

	interval time.Duration
	disabled map[string]bool
}

// Default returns the built-in settings.
func Default() *Config {
	return &Config{
		Interval:               "1m",
		Concurrency:            4,
		HubName:                "local-cluster",
		HubNamespace:           "open-cluster-management",
		BackupNamespace:        "open-cluster-management-backup",
		ObservabilityNamespace: "open-cluster-management-observability",
		ProbePolicy:            "fleet-validator-spoke-probes",
		RequiredAddons:         []string{"work-manager", "governance-policy-framework", "config-policy-controller"},
		Governance: Governance{
			Default: GovernanceThresholds{MinOperatorPolicies: 1, MinConfigurationPolicies: 1},
		},
		Thresholds: Thresholds{
			APIP99Seconds:          1,
			EtcdDBWarnBytes:        1 << 30,
			EtcdDBFailBytes:        6 << 30,
			LeaseStaleSeconds:      300,
			CSRPendingGraceMinutes: 15,
			BackupMaxAgeHours:      26,
		},
		PrometheusURL: "https://thanos-querier.openshift-monitoring.svc:9091",
	}
}

// Load reads path over the defaults; an empty path returns the defaults.
func Load(path string) (*Config, error) {
	c := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return c, c.finish()
}

func (c *Config) finish() error {
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("interval %q: %w", c.Interval, err)
	}
	if d < 10*time.Second {
		return fmt.Errorf("interval %s is below 10s", d)
	}
	c.interval = d
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
	c.disabled = map[string]bool{}
	for _, id := range c.DisabledChecks {
		c.disabled[id] = true
	}
	return nil
}

// IntervalDuration is the parsed Interval.
func (c *Config) IntervalDuration() time.Duration { return c.interval }

// Disabled reports whether a check id is switched off.
func (c *Config) Disabled(id string) bool { return c.disabled[id] }

// IsProbePolicy reports whether root (a replicated policy's root-policy label, namespace.name)
// is the probe policy.
func (c *Config) IsProbePolicy(root string) bool {
	if strings.Contains(c.ProbePolicy, ".") {
		return root == c.ProbePolicy
	}
	_, name, ok := strings.Cut(root, ".")
	return ok && name == c.ProbePolicy
}

// GovernanceFor resolves thresholds: default, then the per-cluster entry, then annotations.
func (c *Config) GovernanceFor(cluster string, annotations map[string]string) GovernanceThresholds {
	g := c.Governance.Default
	if o, ok := c.Governance.Clusters[cluster]; ok {
		g = o
	}
	if v, err := strconv.Atoi(annotations[AnnotationMinOperatorPolicies]); err == nil {
		g.MinOperatorPolicies = v
	}
	if v, err := strconv.Atoi(annotations[AnnotationMinConfigurationPolicies]); err == nil {
		g.MinConfigurationPolicies = v
	}
	return g
}
