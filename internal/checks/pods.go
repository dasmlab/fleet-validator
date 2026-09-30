package checks

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podReady(p *corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// namespacePods passes when every non-completed pod in ns is Running and Ready.
func namespacePods(ctx context.Context, t *Target, ns, selector string) (State, string) {
	pods, err := t.Env.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return skipOrFail(err, "pods in "+ns)
	}
	if len(pods.Items) == 0 {
		return Skip, "no pods in " + ns
	}
	var bad []string
	total := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		total++
		if !podReady(p) {
			bad = append(bad, p.Name+waitingReason(p))
		}
	}
	if len(bad) > 0 {
		return Fail, fmt.Sprintf("%d/%d not ready: %s", len(bad), total, names(bad, 4))
	}
	return Pass, fmt.Sprintf("%d pods Running", total)
}

func waitingReason(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return " (" + cs.State.Waiting.Reason + ")"
		}
	}
	if p.Status.Phase != corev1.PodRunning {
		return " (" + string(p.Status.Phase) + ")"
	}
	return ""
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(strings.ReplaceAll(s, " ", "")),
		strings.ToLower(strings.ReplaceAll(sub, " ", "")))
}

// csvSucceeded passes when a CSV whose name starts with prefix is in phase Succeeded in ns.
func csvSucceeded(ctx context.Context, t *Target, ns, prefix string) (State, string) {
	csvs, err := t.list(ctx, gvrCSV, ns)
	if err != nil {
		return skipOrFail(err, "ClusterServiceVersions")
	}
	for _, c := range csvs {
		if !strings.HasPrefix(c.GetName(), prefix) {
			continue
		}
		phase := nestedStr(c.Object, "status", "phase")
		if phase == "Succeeded" {
			return Pass, c.GetName() + " Succeeded"
		}
		return Fail, fmt.Sprintf("%s phase %s", c.GetName(), phase)
	}
	return Fail, fmt.Sprintf("no %s CSV in %s", prefix, ns)
}
