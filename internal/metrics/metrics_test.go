package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/dasmlab/fleet-validator/internal/checks"
)

func TestSetAndDelete(t *testing.T) {
	reg := prometheus.NewRegistry()
	e := New(reg, "v0.0.1")
	e.Set(checks.Report{
		Cluster: "mo-lab", Type: checks.Managed, Score: 80, Ready: true, CheckedAt: time.Now(),
		GroupScores: map[string]float64{"Platform": 80},
		Counts:      map[checks.State]int{checks.Pass: 1, checks.Fail: 1},
		Results: []checks.Result{
			{ID: "mc.a", Group: "Platform", Title: "A", Severity: checks.Critical, State: checks.Pass},
			{ID: "mc.b", Group: "Platform", Title: "B", Severity: checks.Warning, State: checks.Fail,
				Detail: strings.Repeat("x", 500)},
		},
	})
	if n := testutil.CollectAndCount(e.checkState); n != 2 {
		t.Errorf("check_state series = %d", n)
	}
	if v := testutil.ToFloat64(e.score.WithLabelValues("mo-lab", "managed")); v != 80 {
		t.Errorf("score = %v", v)
	}
	for _, name := range Names {
		if !strings.HasPrefix(name, ns+"_") {
			t.Errorf("bad name %s", name)
		}
	}
	e.Delete("mo-lab")
	if n := testutil.CollectAndCount(e.checkState); n != 0 {
		t.Errorf("after delete = %d", n)
	}
	if got := len([]rune(truncate(strings.Repeat("y", 500)))); got != maxDetail {
		t.Errorf("truncate len %d", got)
	}
}
