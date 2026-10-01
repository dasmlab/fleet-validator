package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Managed-cluster checks. Everything is read on the hub: the ManagedCluster, its lease,
// add-ons and ManagedClusterInfo, plus the fleet-validator probe policy, whose inform-only
// ConfigurationPolicies evaluate the spoke's OpenShift baseline on the spoke itself.

func registrationCond(typ string, optional bool) func(context.Context, *Target) (State, string) {
	return func(_ context.Context, t *Target) (State, string) {
		if t.ManagedCluster == nil {
			return Skip, "no ManagedCluster"
		}
		c, ok := findCond(conditions(t.ManagedCluster.Object), typ)
		if !ok {
			if optional {
				return Skip, typ + " not reported by this ACM version"
			}
			return Fail, typ + " missing"
		}
		if c.Status != "True" {
			return Fail, fmt.Sprintf("%s=%s: %s", typ, c.Status, c.Message)
		}
		return Pass, c.Reason
	}
}

func checkLease(ctx context.Context, t *Target) (State, string) {
	l, err := t.Env.Kube.CoordinationV1().Leases(t.Name).Get(ctx, "managed-cluster-lease", metav1.GetOptions{})
	if err != nil {
		return skipOrFail(err, "managed-cluster-lease")
	}
	if l.Spec.RenewTime == nil {
		return Fail, "lease never renewed"
	}
	age := time.Since(l.Spec.RenewTime.Time).Round(time.Second)
	if age > time.Duration(t.Env.Cfg.Thresholds.LeaseStaleSeconds)*time.Second {
		return Fail, fmt.Sprintf("registration agent lease last renewed %s ago", age)
	}
	return Pass, fmt.Sprintf("renewed %s ago", age)
}

func checkRequiredAddons(ctx context.Context, t *Target) (State, string) {
	addons, err := t.list(ctx, gvrAddOn, t.Name)
	if err != nil {
		return skipOrFail(err, "ManagedClusterAddOns")
	}
	have := map[string]bool{}
	for _, a := range addons {
		have[a.GetName()] = true
	}
	var missing []string
	for _, r := range t.Env.Cfg.RequiredAddons {
		if !have[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return Fail, "missing: " + names(missing, 6)
	}
	return Pass, strings.Join(t.Env.Cfg.RequiredAddons, ", ")
}

func checkAddonsHealthy(ctx context.Context, t *Target) (State, string) {
	addons, err := t.list(ctx, gvrAddOn, t.Name)
	if err != nil {
		return skipOrFail(err, "ManagedClusterAddOns")
	}
	if len(addons) == 0 {
		return Fail, "no add-ons"
	}
	var bad, degraded []string
	for _, a := range addons {
		conds := conditions(a.Object)
		switch {
		case !isTrue(conds, "Available"):
			bad = append(bad, a.GetName())
		case isTrue(conds, "Degraded"):
			degraded = append(degraded, a.GetName())
		}
	}
	if len(bad) > 0 {
		return Fail, fmt.Sprintf("%d/%d unavailable: %s", len(bad), len(addons), names(bad, 5))
	}
	if len(degraded) > 0 {
		return Warn, "degraded: " + names(degraded, 5)
	}
	return Pass, fmt.Sprintf("%d add-ons Available", len(addons))
}

func (t *Target) clusterInfo(ctx context.Context) (*unstructured.Unstructured, error) {
	return t.get(ctx, gvrClusterInfo, t.Name, t.Name)
}

func checkSpokeVersion(ctx context.Context, t *Target) (State, string) {
	ci, err := t.clusterInfo(ctx)
	if err != nil {
		return skipOrFail(err, "ManagedClusterInfo")
	}
	ver := nestedStr(ci.Object, "status", "distributionInfo", "ocp", "version")
	if failed, _, _ := unstructured.NestedBool(ci.Object, "status", "distributionInfo", "ocp", "upgradeFailed"); failed {
		return Fail, "upgrade failed (current " + ver + ")"
	}
	desired := nestedStr(ci.Object, "status", "distributionInfo", "ocp", "desiredVersion")
	if desired != "" && ver != "" && desired != ver {
		return Warn, fmt.Sprintf("upgrading %s -> %s", ver, desired)
	}
	if ver == "" {
		return Skip, "not an OpenShift cluster (" + nestedStr(ci.Object, "status", "kubeVendor") + ")"
	}
	return Pass, "OpenShift " + ver
}

func checkSpokeNodes(ctx context.Context, t *Target) (State, string) {
	ci, err := t.clusterInfo(ctx)
	if err != nil {
		return skipOrFail(err, "ManagedClusterInfo")
	}
	nodes, _, _ := unstructured.NestedSlice(ci.Object, "status", "nodeList")
	if len(nodes) == 0 {
		return Skip, "ManagedClusterInfo has no nodeList yet"
	}
	var notReady []string
	for _, raw := range nodes {
		n, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !isTrue(conditions(n, "conditions"), "Ready") {
			notReady = append(notReady, str(n["name"]))
		}
	}
	if len(notReady) > 0 {
		return Fail, fmt.Sprintf("%d/%d NotReady: %s", len(notReady), len(nodes), names(notReady, 5))
	}
	return Pass, fmt.Sprintf("%d nodes Ready", len(nodes))
}

// probe reads one ConfigurationPolicy result out of the replicated probe policy.
func probe(template string) func(context.Context, *Target) (State, string) {
	return func(ctx context.Context, t *Target) (State, string) {
		items, err := t.list(ctx, gvrPolicy, t.Name)
		if err != nil {
			return skipOrFail(err, "probe policy")
		}
		var p *unstructured.Unstructured
		for i := range items {
			if t.Env.Cfg.IsProbePolicy(items[i].GetLabels()[labelRootPolicy]) {
				p = &items[i]
				break
			}
		}
		if p == nil {
			return Skip, "probe policy " + t.Env.Cfg.ProbePolicy + " not placed on this cluster"
		}
		details, _, _ := unstructured.NestedSlice(p.Object, "status", "details")
		for _, raw := range details {
			d, ok := raw.(map[string]any)
			if !ok || nestedStr(d, "templateMeta", "name") != template {
				continue
			}
			msg := ""
			if h, _, _ := unstructured.NestedSlice(d, "history"); len(h) > 0 {
				if m, ok := h[0].(map[string]any); ok {
					msg = str(m["message"])
				}
			}
			msg = strings.TrimPrefix(strings.TrimPrefix(msg, "Compliant; "), "NonCompliant; ")
			switch str(d["compliant"]) {
			case "Compliant":
				return Pass, msg
			case "NonCompliant":
				return Fail, msg
			}
			return Skip, "probe not evaluated yet"
		}
		return Skip, "probe " + template + " not reported yet"
	}
}
