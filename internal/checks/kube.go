package checks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVRs read by the checks.
var (
	gvrManagedCluster     = schema.GroupVersionResource{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}
	gvrAddOn              = schema.GroupVersionResource{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "managedclusteraddons"}
	gvrClusterInfo        = schema.GroupVersionResource{Group: "internal.open-cluster-management.io", Version: "v1beta1", Resource: "managedclusterinfos"}
	gvrPolicy             = schema.GroupVersionResource{Group: "policy.open-cluster-management.io", Version: "v1", Resource: "policies"}
	gvrMCH                = schema.GroupVersionResource{Group: "operator.open-cluster-management.io", Version: "v1", Resource: "multiclusterhubs"}
	gvrMCE                = schema.GroupVersionResource{Group: "multicluster.openshift.io", Version: "v1", Resource: "multiclusterengines"}
	gvrMCO                = schema.GroupVersionResource{Group: "observability.open-cluster-management.io", Version: "v1beta2", Resource: "multiclusterobservabilities"}
	gvrCSV                = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "clusterserviceversions"}
	gvrBSL                = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}
	gvrBackup             = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}
	gvrBackupSchedule     = schema.GroupVersionResource{Group: "cluster.open-cluster-management.io", Version: "v1beta1", Resource: "backupschedules"}
	gvrArgoApp            = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	gvrArgoCD             = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1beta1", Resource: "argocds"}
	gvrGitOpsCluster      = schema.GroupVersionResource{Group: "apps.open-cluster-management.io", Version: "v1beta1", Resource: "gitopsclusters"}
	gvrClusterVersion     = schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "clusterversions"}
	gvrClusterOperator    = schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "clusteroperators"}
	gvrMCP                = schema.GroupVersionResource{Group: "machineconfiguration.openshift.io", Version: "v1", Resource: "machineconfigpools"}
	gvrEtcd               = schema.GroupVersionResource{Group: "operator.openshift.io", Version: "v1", Resource: "etcds"}
	gvrPlacementDecisions = schema.GroupVersionResource{Group: "cluster.open-cluster-management.io", Version: "v1beta1", Resource: "placementdecisions"}
)

// errNoAPI means the resource type is not served on this cluster (CRD/operator not installed).
var errNoAPI = errors.New("API not installed")

func (t *Target) list(ctx context.Context, gvr schema.GroupVersionResource, ns string) ([]unstructured.Unstructured, error) {
	key := fmt.Sprintf("list/%s/%s", gvr.String(), ns)
	v, err := t.once(key, func() (any, error) {
		l, err := t.Env.Dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil, errNoAPI
			}
			return nil, err
		}
		return l.Items, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]unstructured.Unstructured), nil
}

func (t *Target) get(ctx context.Context, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	key := fmt.Sprintf("get/%s/%s/%s", gvr.String(), ns, name)
	v, err := t.once(key, func() (any, error) {
		o, err := t.Env.Dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if meta.IsNoMatchError(err) || isMissingResource(err) {
			return nil, errNoAPI
		}
		return o, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*unstructured.Unstructured), nil
}

// isMissingResource tells a 404 for an unserved resource type (no details) from a 404 for a
// missing object (details carry the object name).
func isMissingResource(err error) bool {
	var se apierrors.APIStatus
	if !apierrors.IsNotFound(err) || !errors.As(err, &se) {
		return false
	}
	d := se.Status().Details
	return d == nil || d.Name == ""
}

// skipOrFail turns a read error into a check outcome: a missing API is "not applicable",
// anything else (RBAC, timeouts) is a failure to validate.
func skipOrFail(err error, what string) (State, string) {
	if errors.Is(err, errNoAPI) {
		return Skip, what + " API not installed"
	}
	if apierrors.IsNotFound(err) {
		return Fail, what + " not found"
	}
	if apierrors.IsForbidden(err) {
		return Skip, "no RBAC to read " + what
	}
	return Fail, fmt.Sprintf("reading %s: %v", what, err)
}

type condition struct {
	Type, Status, Reason, Message string
}

func conditions(obj map[string]any, path ...string) []condition {
	if len(path) == 0 {
		path = []string{"status", "conditions"}
	}
	raw, _, _ := unstructured.NestedSlice(obj, path...)
	out := make([]condition, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, condition{
			Type: str(m["type"]), Status: str(m["status"]), Reason: str(m["reason"]), Message: str(m["message"]),
		})
	}
	return out
}

func findCond(conds []condition, typ string) (condition, bool) {
	for _, c := range conds {
		if c.Type == typ {
			return c, true
		}
	}
	return condition{}, false
}

func isTrue(conds []condition, typ string) bool {
	c, ok := findCond(conds, typ)
	return ok && c.Status == "True"
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func nestedStr(obj map[string]any, path ...string) string {
	s, _, _ := unstructured.NestedString(obj, path...)
	return s
}

// names formats a short, stable list for check details.
func names(items []string, max int) string {
	sort.Strings(items)
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(items[:max], ", "), len(items)-max)
}
