package checks

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dasmlab/fleet-validator/internal/promq"
)

// OpenShift baseline, read directly on the hub.

func checkClusterVersion(ctx context.Context, t *Target) (State, string) {
	cv, err := t.get(ctx, gvrClusterVersion, "", "version")
	if err != nil {
		return skipOrFail(err, "ClusterVersion")
	}
	conds := conditions(cv.Object)
	ver := nestedStr(cv.Object, "status", "desired", "version")
	if c, ok := findCond(conds, "Failing"); ok && c.Status == "True" {
		return Fail, fmt.Sprintf("%s failing: %s", ver, c.Message)
	}
	if !isTrue(conds, "Available") {
		return Fail, ver + " not Available"
	}
	if isTrue(conds, "Progressing") {
		c, _ := findCond(conds, "Progressing")
		return Warn, fmt.Sprintf("update in progress: %s", c.Message)
	}
	return Pass, "OpenShift " + ver
}

func checkClusterOperators(ctx context.Context, t *Target) (State, string) {
	cos, err := t.list(ctx, gvrClusterOperator, "")
	if err != nil {
		return skipOrFail(err, "ClusterOperators")
	}
	var bad, progressing []string
	for _, co := range cos {
		conds := conditions(co.Object)
		switch {
		case !isTrue(conds, "Available") || isTrue(conds, "Degraded"):
			bad = append(bad, co.GetName())
		case isTrue(conds, "Progressing"):
			progressing = append(progressing, co.GetName())
		}
	}
	if len(bad) > 0 {
		return Fail, fmt.Sprintf("%d unavailable/degraded: %s", len(bad), names(bad, 6))
	}
	if len(progressing) > 0 {
		return Warn, fmt.Sprintf("%d progressing: %s", len(progressing), names(progressing, 6))
	}
	return Pass, fmt.Sprintf("%d operators Available, none Degraded", len(cos))
}

func checkMCP(ctx context.Context, t *Target) (State, string) {
	pools, err := t.list(ctx, gvrMCP, "")
	if err != nil {
		return skipOrFail(err, "MachineConfigPools")
	}
	var degraded, updating []string
	for _, p := range pools {
		conds := conditions(p.Object)
		switch {
		case isTrue(conds, "Degraded"):
			degraded = append(degraded, p.GetName())
		case !isTrue(conds, "Updated"):
			updating = append(updating, p.GetName())
		}
	}
	if len(degraded) > 0 {
		return Fail, "degraded: " + names(degraded, 6)
	}
	if len(updating) > 0 {
		return Warn, "updating: " + names(updating, 6)
	}
	return Pass, fmt.Sprintf("%d pools Updated", len(pools))
}

func checkNodes(ctx context.Context, t *Target) (State, string) {
	nodes, err := t.Env.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return skipOrFail(err, "Nodes")
	}
	var notReady, pressure, cordoned []string
	for _, n := range nodes.Items {
		for _, c := range n.Status.Conditions {
			switch {
			case c.Type == corev1.NodeReady && c.Status != corev1.ConditionTrue:
				notReady = append(notReady, n.Name)
			case c.Type != corev1.NodeReady && c.Status == corev1.ConditionTrue:
				pressure = append(pressure, n.Name+"/"+string(c.Type))
			}
		}
		if n.Spec.Unschedulable {
			cordoned = append(cordoned, n.Name)
		}
	}
	if len(notReady) > 0 {
		return Fail, "NotReady: " + names(notReady, 6)
	}
	if len(pressure) > 0 || len(cordoned) > 0 {
		return Warn, fmt.Sprintf("pressure: [%s] cordoned: [%s]", names(pressure, 4), names(cordoned, 4))
	}
	return Pass, fmt.Sprintf("%d nodes Ready", len(nodes.Items))
}

func checkPendingCSRs(ctx context.Context, t *Target) (State, string) {
	csrs, err := t.Env.Kube.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{})
	if err != nil {
		return skipOrFail(err, "CSRs")
	}
	grace := time.Duration(t.Env.Cfg.Thresholds.CSRPendingGraceMinutes) * time.Minute
	var stale, fresh []string
	for _, c := range csrs.Items {
		if len(c.Status.Conditions) > 0 {
			continue
		}
		if time.Since(c.CreationTimestamp.Time) > grace {
			stale = append(stale, c.Name)
		} else {
			fresh = append(fresh, c.Name)
		}
	}
	if len(stale) > 0 {
		return Fail, fmt.Sprintf("%d pending > %s: %s", len(stale), grace, names(stale, 4))
	}
	if len(fresh) > 0 {
		return Warn, fmt.Sprintf("%d pending (within %s grace)", len(fresh), grace)
	}
	return Pass, "no pending CSRs"
}

func checkEtcdMembers(ctx context.Context, t *Target) (State, string) {
	pods, err := t.Env.Kube.CoreV1().Pods("openshift-etcd").List(ctx, metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return skipOrFail(err, "etcd pods")
	}
	if len(pods.Items) == 0 {
		return Fail, "no etcd pods in openshift-etcd"
	}
	var bad []string
	for i := range pods.Items {
		if !podReady(&pods.Items[i]) {
			bad = append(bad, pods.Items[i].Name)
		}
	}
	if len(bad) > 0 {
		return Fail, "not ready: " + names(bad, 5)
	}
	if e, err := t.get(ctx, gvrEtcd, "", "cluster"); err == nil {
		if c, ok := findCond(conditions(e.Object), "EtcdMembersAvailable"); ok && c.Status != "True" {
			return Fail, "EtcdMembersAvailable=False: " + c.Message
		}
	}
	return Pass, fmt.Sprintf("%d etcd members ready", len(pods.Items))
}

func checkEtcdDBSize(ctx context.Context, t *Target) (State, string) {
	if t.Env.Prom == nil {
		return Skip, "Prometheus queries disabled"
	}
	v, err := t.Env.Prom.Scalar(ctx, `max(etcd_mvcc_db_total_size_in_bytes{job="etcd"})`)
	if err != nil {
		return promSkip(err)
	}
	th := t.Env.Cfg.Thresholds
	d := fmt.Sprintf("largest member DB %s", humanBytes(v))
	switch {
	case v >= float64(th.EtcdDBFailBytes):
		return Fail, d
	case v >= float64(th.EtcdDBWarnBytes):
		return Warn, fmt.Sprintf("%s (warn at %s)", d, humanBytes(float64(th.EtcdDBWarnBytes)))
	}
	return Pass, d
}

func checkAPILatency(ctx context.Context, t *Target) (State, string) {
	if t.Env.Prom == nil {
		return Skip, "Prometheus queries disabled"
	}
	v, err := t.Env.Prom.Scalar(ctx, `histogram_quantile(0.99, sum by (le) (rate(apiserver_request_duration_seconds_bucket{`+
		`job="apiserver",verb=~"GET|POST|PUT|PATCH|DELETE",subresource!~"log|exec|portforward|attach|proxy"}[5m])))`)
	if err != nil {
		return promSkip(err)
	}
	limit := t.Env.Cfg.Thresholds.APIP99Seconds
	d := fmt.Sprintf("p99 %.0fms (limit %.0fms)", v*1000, limit*1000)
	if v > limit {
		return Fail, d
	}
	return Pass, d
}

func checkUWM(ctx context.Context, t *Target) (State, string) {
	cm, err := t.Env.Kube.CoreV1().ConfigMaps("openshift-monitoring").Get(ctx, "cluster-monitoring-config", metav1.GetOptions{})
	if err != nil {
		st, d := skipOrFail(err, "cluster-monitoring-config")
		if st == Fail {
			return Warn, d
		}
		return st, d
	}
	if !containsFold(cm.Data["config.yaml"], "enableUserWorkload: true") {
		return Warn, "User Workload Monitoring disabled (fleet-validator metrics will not reach Grafana)"
	}
	return Pass, "enableUserWorkload: true"
}

func promSkip(err error) (State, string) {
	if errors.Is(err, promq.ErrNoData) {
		return Skip, "metric not available"
	}
	return Skip, "Prometheus: " + err.Error()
}

func humanBytes(v float64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%.0fB", v)
	}
	div, exp := float64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", v/div, "KMGTPE"[exp])
}
