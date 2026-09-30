package checks

import (
	"os"
	"strings"
	"testing"
)

// The hub reads probe results by ConfigurationPolicy name, so the shipped policy must use
// exactly the names the checks look for.
func TestProbePolicyNames(t *testing.T) {
	b, err := os.ReadFile("../../deploy/acm/policy-spoke-probes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ProbeCOAvailable, ProbeCODegraded, ProbeMCPDegraded, ProbeMCPUpdated,
		ProbeCSR, ProbeEtcd, ProbeKlusterlet} {
		if !strings.Contains(string(b), "name: "+name+"\n") {
			t.Errorf("probe %s missing from policy-spoke-probes.yaml", name)
		}
	}
	if !strings.Contains(string(b), "name: fleet-validator-spoke-probes\n  namespace: open-cluster-management-global-set") {
		t.Error("probe policy name/namespace no longer matches config.ProbePolicy default")
	}
}
