package runner

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/dasmlab/fleet-validator/internal/checks"
	"github.com/dasmlab/fleet-validator/internal/config"
	"github.com/dasmlab/fleet-validator/internal/metrics"
)

func mc(name string, labels, ann map[string]string, accepted, joinedOK bool) runtime.Object {
	b := func(v bool) string {
		if v {
			return "True"
		}
		return "False"
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.open-cluster-management.io/v1", "kind": "ManagedCluster",
		"metadata": map[string]any{"name": name},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "HubAcceptedManagedCluster", "status": b(accepted)},
			map[string]any{"type": "ManagedClusterJoined", "status": b(joinedOK)},
		}},
	}}
	u.SetLabels(labels)
	u.SetAnnotations(ann)
	return u
}

func TestDiscover(t *testing.T) {
	cfg, _ := config.Load("")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvrManagedCluster: "ManagedClusterList"},
		mc("hub-a", map[string]string{"local-cluster": "true"}, nil, true, true),
		mc("zeta", nil, nil, true, true),
		mc("alpha", nil, nil, true, true),
		mc("pending", nil, nil, true, false),
		mc("not-accepted", nil, nil, false, true),
		mc("skipped", nil, map[string]string{config.AnnotationSkip: "true"}, true, true),
	)
	r := New(&checks.Env{Dyn: dyn, Cfg: cfg}, metrics.New(prometheus.NewRegistry(), "test"), logr.Discard())
	got, err := r.discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		typ  checks.ClusterType
	}{{"hub-a", checks.Hub}, {"alpha", checks.Managed}, {"zeta", checks.Managed}}
	if len(got) != len(want) {
		t.Fatalf("got %d targets: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].name != w.name || got[i].typ != w.typ {
			t.Errorf("target %d = %s/%s, want %s/%s", i, got[i].name, got[i].typ, w.name, w.typ)
		}
	}
}
