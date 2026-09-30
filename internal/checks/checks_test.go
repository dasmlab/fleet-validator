package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/dasmlab/fleet-validator/internal/config"
)

const probeRoot = "open-cluster-management-global-set.fleet-validator-spoke-probes"

func listKinds() map[schema.GroupVersionResource]string {
	m := map[schema.GroupVersionResource]string{}
	for gvr, kind := range map[schema.GroupVersionResource]string{
		gvrManagedCluster: "ManagedCluster", gvrAddOn: "ManagedClusterAddOn", gvrClusterInfo: "ManagedClusterInfo",
		gvrPolicy: "Policy", gvrMCH: "MultiClusterHub", gvrMCE: "MultiClusterEngine",
		gvrMCO: "MultiClusterObservability", gvrCSV: "ClusterServiceVersion", gvrBSL: "BackupStorageLocation",
		gvrBackup: "Backup", gvrBackupSchedule: "BackupSchedule", gvrArgoApp: "Application", gvrArgoCD: "ArgoCD",
		gvrGitOpsCluster: "GitOpsCluster", gvrClusterVersion: "ClusterVersion", gvrClusterOperator: "ClusterOperator",
		gvrMCP: "MachineConfigPool", gvrEtcd: "Etcd", gvrPlacementDecisions: "PlacementDecision",
	} {
		m[gvr] = kind + "List"
	}
	return m
}

func obj(apiVersion, kind, ns, name string, fields map[string]any) *unstructured.Unstructured {
	o := map[string]any{"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": ns}}
	for k, v := range fields {
		o[k] = v
	}
	return &unstructured.Unstructured{Object: o}
}

func conds(pairs ...string) []any {
	out := []any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]any{"type": pairs[i], "status": pairs[i+1], "reason": "R", "message": "m"})
	}
	return out
}

func newTarget(t *testing.T, name string, typ ClusterType, mc *unstructured.Unstructured, kube []runtime.Object,
	objs ...runtime.Object) *Target {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{
		Dyn:  dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds(), objs...),
		Kube: kubefake.NewSimpleClientset(kube...),
		Cfg:  cfg,
	}
	return NewTarget(name, typ, mc, env)
}

func policy(ns, name, root, compliant string, kinds ...string) *unstructured.Unstructured {
	tmpls := make([]any, 0, len(kinds))
	for _, k := range kinds {
		tmpls = append(tmpls, map[string]any{"objectDefinition": map[string]any{"kind": k}})
	}
	p := obj("policy.open-cluster-management.io/v1", "Policy", ns, name, map[string]any{
		"spec":   map[string]any{"policy-templates": tmpls},
		"status": map[string]any{"compliant": compliant},
	})
	if root != "" {
		p.SetLabels(map[string]string{labelRootPolicy: root})
	}
	return p
}

func TestMCHChecks(t *testing.T) {
	mch := obj("operator.open-cluster-management.io/v1", "MultiClusterHub", "open-cluster-management", "multiclusterhub",
		map[string]any{
			"spec": map[string]any{"overrides": map[string]any{"components": []any{
				map[string]any{"name": "cluster-backup", "enabled": true}}}},
			"status": map[string]any{"phase": "Running", "currentVersion": "2.14.1", "components": map[string]any{
				"grc":         map[string]any{"status": "True", "type": "Available"},
				"search-v2":   map[string]any{"status": "False", "type": "Available", "reason": "Deploying"},
				"console-mce": map[string]any{"status": "True", "type": "Available"},
			}},
		})
	tg := newTarget(t, "local-cluster", Hub, nil, nil, mch)
	ctx := context.Background()

	if st, d := checkMCHPhase(ctx, tg); st != Pass || !strings.Contains(d, "2.14.1") {
		t.Errorf("phase: %s %q", st, d)
	}
	if st, d := checkMCHComponents(ctx, tg); st != Fail || !strings.Contains(d, "search-v2") {
		t.Errorf("components: %s %q", st, d)
	}
	if st, _ := checkBackupEnabled(ctx, tg); st != Pass {
		t.Errorf("backup enabled: %s", st)
	}
}

func TestMissingAPIIsSkip(t *testing.T) {
	tg := newTarget(t, "local-cluster", Hub, nil, nil)
	tg.Env.Dyn = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	// The plain fake client panics on unregistered list kinds; safeRun must turn that into a result.
	st, _ := safeRun(context.Background(), Check{Run: checkMCO}, tg)
	if st != Fail && st != Skip {
		t.Errorf("got %s", st)
	}
}

func TestGovernance(t *testing.T) {
	mc := obj("cluster.open-cluster-management.io/v1", "ManagedCluster", "", "mo-lab", nil)
	mc.SetAnnotations(map[string]string{config.AnnotationMinConfigurationPolicies: "3"})
	tg := newTarget(t, "mo-lab", Managed, mc, nil,
		policy("mo-lab", "mo-lab.ops", "mo-lab.ops", "Compliant", "OperatorPolicy", "ConfigurationPolicy"),
		policy("mo-lab", "mo-lab.cfg", "mo-lab.cfg", "NonCompliant", "ConfigurationPolicy"),
		policy("mo-lab", probeRoot, probeRoot, "NonCompliant", "ConfigurationPolicy", "ConfigurationPolicy"),
		policy("mo-lab", "unrelated", "", "Compliant", "ConfigurationPolicy"),
	)
	ctx := context.Background()

	if st, d := checkPoliciesBound(ctx, tg); st != Pass || !strings.HasPrefix(d, "2 ") {
		t.Errorf("bound: %s %q", st, d)
	}
	if st, _ := checkPolicyKindCount("OperatorPolicy")(ctx, tg); st != Pass {
		t.Errorf("operator: %s", st)
	}
	// 2 ConfigurationPolicies (probe excluded) against the annotation's minimum of 3.
	if st, d := checkPolicyKindCount("ConfigurationPolicy")(ctx, tg); st != Fail || !strings.HasPrefix(d, "2 ") {
		t.Errorf("configuration: %s %q", st, d)
	}
	if st, d := checkPoliciesCompliant(ctx, tg); st != Fail || !strings.Contains(d, "mo-lab.cfg") {
		t.Errorf("compliant: %s %q", st, d)
	}
}

func TestProbe(t *testing.T) {
	p := policy("mo-lab", probeRoot, probeRoot, "NonCompliant")
	_ = unstructured.SetNestedSlice(p.Object, []any{
		map[string]any{"templateMeta": map[string]any{"name": ProbeCODegraded}, "compliant": "NonCompliant",
			"history": []any{map[string]any{"message": "NonCompliant; violation - clusteroperators found: [dns]"}}},
		map[string]any{"templateMeta": map[string]any{"name": ProbeCSR}, "compliant": "Compliant",
			"history": []any{map[string]any{"message": "Compliant; notification - no instances"}}},
	}, "status", "details")
	tg := newTarget(t, "mo-lab", Managed, nil, nil, p)
	ctx := context.Background()

	if st, d := probe(ProbeCODegraded)(ctx, tg); st != Fail || !strings.HasPrefix(d, "violation") {
		t.Errorf("co: %s %q", st, d)
	}
	if st, _ := probe(ProbeCSR)(ctx, tg); st != Pass {
		t.Errorf("csr: %s", st)
	}
	if st, _ := probe(ProbeEtcd)(ctx, tg); st != Skip {
		t.Errorf("etcd not reported: %s", st)
	}
	other := newTarget(t, "ag-prod", Managed, nil, nil)
	if st, d := probe(ProbeCSR)(ctx, other); st != Skip || !strings.Contains(d, "not placed") {
		t.Errorf("unplaced: %s %q", st, d)
	}
}

func TestRegistrationAndLease(t *testing.T) {
	mc := obj("cluster.open-cluster-management.io/v1", "ManagedCluster", "", "mo-lab", map[string]any{
		"status": map[string]any{"conditions": conds(
			"HubAcceptedManagedCluster", "True", "ManagedClusterJoined", "True",
			"ManagedClusterConditionAvailable", "Unknown")},
	})
	stale := metav1.NewMicroTime(time.Now().Add(-10 * time.Minute))
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "managed-cluster-lease", Namespace: "mo-lab"},
		Spec:       coordinationv1.LeaseSpec{RenewTime: &stale},
	}
	tg := newTarget(t, "mo-lab", Managed, mc, []runtime.Object{lease})
	ctx := context.Background()

	if st, _ := registrationCond("ManagedClusterJoined", false)(ctx, tg); st != Pass {
		t.Errorf("joined: %s", st)
	}
	if st, _ := registrationCond("ManagedClusterConditionAvailable", false)(ctx, tg); st != Fail {
		t.Errorf("available: %s", st)
	}
	if st, _ := registrationCond("ManagedClusterImportSucceeded", true)(ctx, tg); st != Skip {
		t.Errorf("import optional: %s", st)
	}
	if st, _ := checkLease(ctx, tg); st != Fail {
		t.Errorf("lease: %s", st)
	}
}

func TestAddonsAndFleetCompliance(t *testing.T) {
	addon := func(name string, c ...string) runtime.Object {
		return obj("addon.open-cluster-management.io/v1alpha1", "ManagedClusterAddOn", "mo-lab", name,
			map[string]any{"status": map[string]any{"conditions": conds(c...)}})
	}
	tg := newTarget(t, "mo-lab", Managed, nil, nil,
		addon("work-manager", "Available", "True"),
		addon("config-policy-controller", "Available", "True", "Degraded", "True"),
		policy("ag-prod", "ns.p1", "ns.p1", "NonCompliant"),
		policy("mo-lab", probeRoot, probeRoot, "NonCompliant"),
	)
	ctx := context.Background()

	if st, d := checkRequiredAddons(ctx, tg); st != Fail || !strings.Contains(d, "governance-policy-framework") {
		t.Errorf("required: %s %q", st, d)
	}
	if st, _ := checkAddonsHealthy(ctx, tg); st != Warn {
		t.Errorf("healthy: %s", st)
	}
	if st, d := checkFleetCompliance(ctx, tg); st != Fail || d != "NonCompliant by cluster: ag-prod:1" {
		t.Errorf("fleet: %s %q", st, d)
	}
}

func TestClusterOperators(t *testing.T) {
	co := func(name string, c ...string) runtime.Object {
		return obj("config.openshift.io/v1", "ClusterOperator", "", name,
			map[string]any{"status": map[string]any{"conditions": conds(c...)}})
	}
	tg := newTarget(t, "local-cluster", Hub, nil, nil,
		co("dns", "Available", "True", "Degraded", "False", "Progressing", "False"),
		co("ingress", "Available", "True", "Degraded", "False", "Progressing", "True"),
	)
	if st, d := checkClusterOperators(context.Background(), tg); st != Warn || !strings.Contains(d, "ingress") {
		t.Errorf("got %s %q", st, d)
	}
}
