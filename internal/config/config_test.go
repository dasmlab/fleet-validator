package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
interval: 2m
concurrency: 0
governance:
  default: {minOperatorPolicies: 1, minConfigurationPolicies: 2}
  clusters:
    local-cluster: {minOperatorPolicies: 2, minConfigurationPolicies: 5}
disabledChecks: [hub.backup.recent]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.IntervalDuration() != 2*time.Minute || c.Concurrency != 1 {
		t.Errorf("interval %s concurrency %d", c.IntervalDuration(), c.Concurrency)
	}
	if !c.Disabled("hub.backup.recent") || c.Disabled("hub.backup.bsl") {
		t.Error("disabled checks")
	}
	if c.Thresholds.APIP99Seconds != 1 {
		t.Error("defaults lost on partial file")
	}
	if g := c.GovernanceFor("mo-lab", nil); g.MinConfigurationPolicies != 2 {
		t.Errorf("default: %+v", g)
	}
	if g := c.GovernanceFor("local-cluster", nil); g.MinConfigurationPolicies != 5 {
		t.Errorf("per cluster: %+v", g)
	}
	g := c.GovernanceFor("local-cluster", map[string]string{AnnotationMinOperatorPolicies: "4"})
	if g.MinOperatorPolicies != 4 || g.MinConfigurationPolicies != 5 {
		t.Errorf("annotation: %+v", g)
	}
}

func TestIsProbePolicy(t *testing.T) {
	c := Default()
	for root, want := range map[string]bool{
		"open-cluster-management.fleet-validator-spoke-probes":            true,
		"open-cluster-management-global-set.fleet-validator-spoke-probes": true,
		"open-cluster-management.hub-fleet-validator":                     false,
		"fleet-validator-spoke-probes":                                    false,
	} {
		if got := c.IsProbePolicy(root); got != want {
			t.Errorf("default %q: got %v", root, got)
		}
	}
	c.ProbePolicy = "policies.fleet-validator-spoke-probes"
	if c.IsProbePolicy("open-cluster-management.fleet-validator-spoke-probes") || !c.IsProbePolicy(c.ProbePolicy) {
		t.Error("namespace.name must match exactly")
	}
}

func TestIntervalFloor(t *testing.T) {
	c := Default()
	c.Interval = "1s"
	if err := c.finish(); err == nil {
		t.Error("want error for 1s interval")
	}
}
