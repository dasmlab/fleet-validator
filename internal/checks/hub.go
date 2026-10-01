package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ACM hub components, backup, observability, GitOps and fleet-wide views.

func (t *Target) mch(ctx context.Context) (*unstructured.Unstructured, error) {
	items, err := t.list(ctx, gvrMCH, t.Env.Cfg.HubNamespace)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no MultiClusterHub in %s", t.Env.Cfg.HubNamespace)
	}
	return &items[0], nil
}

func checkMCHPhase(ctx context.Context, t *Target) (State, string) {
	m, err := t.mch(ctx)
	if err != nil {
		return skipOrFail(err, "MultiClusterHub")
	}
	phase := nestedStr(m.Object, "status", "phase")
	ver := nestedStr(m.Object, "status", "currentVersion")
	if phase != "Running" {
		return Fail, fmt.Sprintf("phase %q (ACM %s)", phase, ver)
	}
	return Pass, "Running, ACM " + ver
}

func checkMCHComponents(ctx context.Context, t *Target) (State, string) {
	m, err := t.mch(ctx)
	if err != nil {
		return skipOrFail(err, "MultiClusterHub")
	}
	comps, _, _ := unstructured.NestedMap(m.Object, "status", "components")
	if len(comps) == 0 {
		return Fail, "status.components is empty"
	}
	var bad []string
	for name, raw := range comps {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if str(c["status"]) != "True" {
			bad = append(bad, fmt.Sprintf("%s(%s)", name, str(c["reason"])))
		}
	}
	if len(bad) > 0 {
		return Fail, fmt.Sprintf("%d/%d not available: %s", len(bad), len(comps), names(bad, 5))
	}
	return Pass, fmt.Sprintf("%d components Available", len(comps))
}

func checkMCE(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrMCE, "")
	if err != nil {
		return skipOrFail(err, "MultiClusterEngine")
	}
	if len(items) == 0 {
		return Fail, "no MultiClusterEngine"
	}
	phase := nestedStr(items[0].Object, "status", "phase")
	if phase != "Available" {
		return Fail, "phase " + phase
	}
	return Pass, "Available, MCE " + nestedStr(items[0].Object, "status", "currentVersion")
}

func checkACMCSV(ctx context.Context, t *Target) (State, string) {
	return csvSucceeded(ctx, t, t.Env.Cfg.HubNamespace, "advanced-cluster-management")
}

func checkHubPods(ctx context.Context, t *Target) (State, string) {
	return namespacePods(ctx, t, t.Env.Cfg.HubNamespace, "")
}

func checkMCEPods(ctx context.Context, t *Target) (State, string) {
	return namespacePods(ctx, t, "multicluster-engine", "")
}

// --- Backup & DR ---

func checkBackupEnabled(ctx context.Context, t *Target) (State, string) {
	m, err := t.mch(ctx)
	if err != nil {
		return skipOrFail(err, "MultiClusterHub")
	}
	comps, _, _ := unstructured.NestedSlice(m.Object, "spec", "overrides", "components")
	for _, raw := range comps {
		c, ok := raw.(map[string]any)
		if ok && str(c["name"]) == "cluster-backup" {
			if b, _ := c["enabled"].(bool); b {
				return Pass, "cluster-backup component enabled"
			}
		}
	}
	return Fail, "cluster-backup component not enabled on the MultiClusterHub"
}

func checkOADP(ctx context.Context, t *Target) (State, string) {
	return csvSucceeded(ctx, t, t.Env.Cfg.BackupNamespace, "oadp-operator")
}

func checkBSL(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrBSL, t.Env.Cfg.BackupNamespace)
	if err != nil {
		return skipOrFail(err, "BackupStorageLocation")
	}
	if len(items) == 0 {
		return Fail, "no BackupStorageLocation in " + t.Env.Cfg.BackupNamespace
	}
	var bad []string
	for _, b := range items {
		if p := nestedStr(b.Object, "status", "phase"); p != "Available" {
			bad = append(bad, fmt.Sprintf("%s(%s)", b.GetName(), p))
		}
	}
	if len(bad) > 0 {
		return Fail, "not Available: " + names(bad, 4)
	}
	return Pass, fmt.Sprintf("%d location(s) Available", len(items))
}

func checkBackupSchedule(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrBackupSchedule, t.Env.Cfg.BackupNamespace)
	if err != nil {
		return skipOrFail(err, "BackupSchedule")
	}
	if len(items) == 0 {
		return Fail, "no BackupSchedule in " + t.Env.Cfg.BackupNamespace
	}
	s := items[0]
	phase := nestedStr(s.Object, "status", "phase")
	switch phase {
	case "Enabled":
		return Pass, s.GetName() + " Enabled"
	case "BackupCollision":
		return Fail, s.GetName() + " BackupCollision: another hub writes to the same storage"
	}
	return Fail, fmt.Sprintf("%s phase %q: %s", s.GetName(), phase, nestedStr(s.Object, "status", "lastMessage"))
}

func checkRecentBackup(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrBackup, t.Env.Cfg.BackupNamespace)
	if err != nil {
		return skipOrFail(err, "velero Backups")
	}
	var latest time.Time
	var latestName string
	for _, b := range items {
		if nestedStr(b.Object, "status", "phase") != "Completed" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, nestedStr(b.Object, "status", "completionTimestamp"))
		if err == nil && ts.After(latest) {
			latest, latestName = ts, b.GetName()
		}
	}
	if latest.IsZero() {
		return Fail, "no Completed backup"
	}
	age := time.Since(latest).Round(time.Minute)
	maxAge := time.Duration(t.Env.Cfg.Thresholds.BackupMaxAgeHours) * time.Hour
	if age > maxAge {
		return Fail, fmt.Sprintf("last Completed backup %s is %s old", latestName, age)
	}
	return Pass, fmt.Sprintf("%s completed %s ago", latestName, age)
}

// checkBackupPolicy reads the Red Hat built-in backup-restore-enabled policy on the hub.
func checkBackupPolicy(ctx context.Context, t *Target) (State, string) {
	p, err := t.get(ctx, gvrPolicy, t.Env.Cfg.BackupNamespace, "backup-restore-enabled")
	if err != nil {
		return skipOrFail(err, "policy backup-restore-enabled")
	}
	c := nestedStr(p.Object, "status", "compliant")
	switch c {
	case "Compliant":
		return Pass, "backup-restore-enabled Compliant"
	case "":
		return Warn, "backup-restore-enabled not evaluated yet"
	}
	return Fail, "backup-restore-enabled " + c
}

// --- Observability ---

func checkMCO(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrMCO, "")
	if err != nil {
		return skipOrFail(err, "MultiClusterObservability")
	}
	if len(items) == 0 {
		return Fail, "no MultiClusterObservability"
	}
	conds := conditions(items[0].Object)
	if isTrue(conds, "Ready") {
		return Pass, items[0].GetName() + " Ready"
	}
	if c, ok := findCond(conds, "Degraded"); ok && c.Status == "True" {
		return Fail, "Degraded: " + c.Message
	}
	return Fail, "not Ready"
}

func checkObservabilityPods(ctx context.Context, t *Target) (State, string) {
	return namespacePods(ctx, t, t.Env.Cfg.ObservabilityNamespace, "")
}

// --- GitOps ---

func checkArgoCD(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrArgoCD, "")
	if err != nil {
		return skipOrFail(err, "ArgoCD")
	}
	if len(items) == 0 {
		return Fail, "no ArgoCD instance"
	}
	var bad []string
	for _, a := range items {
		if p := nestedStr(a.Object, "status", "phase"); p != "Available" {
			bad = append(bad, fmt.Sprintf("%s/%s(%s)", a.GetNamespace(), a.GetName(), p))
		}
	}
	if len(bad) > 0 {
		return Fail, "not Available: " + names(bad, 4)
	}
	return Pass, fmt.Sprintf("%d instance(s) Available", len(items))
}

func checkArgoApps(field string) func(context.Context, *Target) (State, string) {
	return func(ctx context.Context, t *Target) (State, string) {
		apps, err := t.list(ctx, gvrArgoApp, "")
		if err != nil {
			return skipOrFail(err, "Argo CD Applications")
		}
		if len(apps) == 0 {
			return Skip, "no Applications"
		}
		var bad, soft []string
		for _, a := range apps {
			n := a.GetNamespace() + "/" + a.GetName()
			switch field {
			case "sync":
				if s := nestedStr(a.Object, "status", "sync", "status"); s != "Synced" {
					bad = append(bad, fmt.Sprintf("%s(%s)", n, s))
				}
			case "health":
				switch s := nestedStr(a.Object, "status", "health", "status"); s {
				case "Healthy":
				case "Progressing", "Suspended":
					soft = append(soft, fmt.Sprintf("%s(%s)", n, s))
				default:
					bad = append(bad, fmt.Sprintf("%s(%s)", n, s))
				}
			}
		}
		if len(bad) > 0 {
			return Fail, fmt.Sprintf("%d/%d: %s", len(bad), len(apps), names(bad, 4))
		}
		if len(soft) > 0 {
			return Warn, fmt.Sprintf("%d/%d: %s", len(soft), len(apps), names(soft, 4))
		}
		if field == "sync" {
			return Pass, fmt.Sprintf("%d Applications Synced", len(apps))
		}
		return Pass, fmt.Sprintf("%d Applications Healthy", len(apps))
	}
}

func checkGitOpsCluster(ctx context.Context, t *Target) (State, string) {
	items, err := t.list(ctx, gvrGitOpsCluster, "")
	if err != nil {
		return skipOrFail(err, "GitOpsCluster")
	}
	if len(items) == 0 {
		return Skip, "no GitOpsCluster (ACM → Argo CD cluster registration not used)"
	}
	var bad []string
	for _, g := range items {
		if p := nestedStr(g.Object, "status", "phase"); p != "successful" {
			bad = append(bad, fmt.Sprintf("%s/%s(%s)", g.GetNamespace(), g.GetName(), p))
		}
	}
	if len(bad) > 0 {
		return Fail, names(bad, 4)
	}
	return Pass, fmt.Sprintf("%d GitOpsCluster(s) successful", len(items))
}

// --- Fleet (hub view of all ManagedClusters) ---

func checkFleetAvailable(ctx context.Context, t *Target) (State, string) {
	mcs, err := t.list(ctx, gvrManagedCluster, "")
	if err != nil {
		return skipOrFail(err, "ManagedClusters")
	}
	var bad []string
	for _, mc := range mcs {
		if !isTrue(conditions(mc.Object), "ManagedClusterConditionAvailable") {
			bad = append(bad, mc.GetName())
		}
	}
	if len(bad) > 0 {
		return Fail, fmt.Sprintf("%d/%d not Available: %s", len(bad), len(mcs), names(bad, 5))
	}
	return Pass, fmt.Sprintf("%d/%d ManagedClusters Available", len(mcs), len(mcs))
}

func checkFleetJoined(ctx context.Context, t *Target) (State, string) {
	mcs, err := t.list(ctx, gvrManagedCluster, "")
	if err != nil {
		return skipOrFail(err, "ManagedClusters")
	}
	var pending []string
	for _, mc := range mcs {
		conds := conditions(mc.Object)
		if !isTrue(conds, "HubAcceptedManagedCluster") || !isTrue(conds, "ManagedClusterJoined") {
			pending = append(pending, mc.GetName())
		}
	}
	if len(pending) > 0 {
		return Warn, fmt.Sprintf("not accepted/joined (not validated): %s", names(pending, 5))
	}
	return Pass, fmt.Sprintf("%d ManagedClusters accepted and joined", len(mcs))
}

// checkRootPolicyPlacement flags enabled root policies that reach no cluster.
func checkRootPolicyPlacement(ctx context.Context, t *Target) (State, string) {
	all, err := t.list(ctx, gvrPolicy, "")
	if err != nil {
		return skipOrFail(err, "Policies")
	}
	var unplaced []string
	roots := 0
	for _, p := range all {
		if _, replicated := p.GetLabels()[labelRootPolicy]; replicated {
			continue
		}
		if d, _, _ := unstructured.NestedBool(p.Object, "spec", "disabled"); d {
			continue
		}
		roots++
		placement, _, _ := unstructured.NestedSlice(p.Object, "status", "placement")
		status, _, _ := unstructured.NestedSlice(p.Object, "status", "status")
		if len(placement) == 0 || len(status) == 0 {
			unplaced = append(unplaced, p.GetNamespace()+"/"+p.GetName())
		}
	}
	if roots == 0 {
		return Fail, "no root policies on the hub"
	}
	if len(unplaced) > 0 {
		return Warn, fmt.Sprintf("%d/%d root policies select no cluster: %s", len(unplaced), roots, names(unplaced, 4))
	}
	return Pass, fmt.Sprintf("%d root policies placed", roots)
}

// checkFleetCompliance summarises replicated policy compliance across every cluster.
func checkFleetCompliance(ctx context.Context, t *Target) (State, string) {
	all, err := t.list(ctx, gvrPolicy, "")
	if err != nil {
		return skipOrFail(err, "Policies")
	}
	byCluster := map[string]int{}
	total := 0
	for _, p := range all {
		root, replicated := p.GetLabels()[labelRootPolicy]
		if !replicated || t.Env.Cfg.IsProbePolicy(root) {
			continue
		}
		total++
		if nestedStr(p.Object, "status", "compliant") == "NonCompliant" {
			byCluster[p.GetNamespace()]++
		}
	}
	if len(byCluster) == 0 {
		return Pass, fmt.Sprintf("%d replicated policies, none NonCompliant", total)
	}
	var parts []string
	for c, n := range byCluster {
		parts = append(parts, fmt.Sprintf("%s:%d", c, n))
	}
	sort.Strings(parts)
	return Fail, "NonCompliant by cluster: " + strings.Join(parts, ", ")
}
