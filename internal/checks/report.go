package checks

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Report is the validation status ("checklist") of one cluster.
type Report struct {
	Cluster     string             `json:"cluster"`
	Type        ClusterType        `json:"type"`
	Score       float64            `json:"score"`
	Ready       bool               `json:"ready"`
	GroupScores map[string]float64 `json:"groupScores"`
	Counts      map[State]int      `json:"counts"`
	Results     []Result           `json:"results"`
	Info        map[string]string  `json:"info,omitempty"`
	CheckedAt   time.Time          `json:"checkedAt"`
	Duration    float64            `json:"durationSeconds"`
}

// perCheckTimeout bounds one check so a slow API call cannot stall a cluster's run.
const perCheckTimeout = 20 * time.Second

// Validate runs every enabled check against t and scores the result.
func Validate(ctx context.Context, t *Target, list []Check) Report {
	start := time.Now()
	r := Report{Cluster: t.Name, Type: t.Type, Counts: map[State]int{}, GroupScores: map[string]float64{}}
	for _, c := range list {
		if t.Env.Cfg.Disabled(c.ID) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
		st, detail := safeRun(cctx, c, t)
		cancel()
		r.Results = append(r.Results, Result{
			ID: c.ID, Group: c.Group, Title: c.Title, Severity: c.Severity,
			State: st, Detail: detail, Command: c.Command,
		})
	}
	r.Score, r.GroupScores, r.Ready = score(r.Results)
	for _, res := range r.Results {
		r.Counts[res.State]++
	}
	r.Info = clusterInfo(ctx, t)
	r.CheckedAt = time.Now()
	r.Duration = time.Since(start).Seconds()
	return r
}

func safeRun(ctx context.Context, c Check, t *Target) (st State, detail string) {
	defer func() {
		if p := recover(); p != nil {
			st, detail = Fail, fmt.Sprintf("check panicked: %v", p)
		}
	}()
	return c.Run(ctx, t)
}

// score weights each check by severity: pass counts fully, warn half, fail nothing, skip is
// left out. A cluster is Ready when no critical check fails.
func score(results []Result) (float64, map[string]float64, bool) {
	type acc struct{ got, max float64 }
	total := acc{}
	groups := map[string]*acc{}
	ready := true
	for _, r := range results {
		if r.State == Skip {
			continue
		}
		w := r.Severity.Weight()
		got := 0.0
		switch r.State {
		case Pass:
			got = w
		case Warn:
			got = w / 2
		case Fail:
			if r.Severity == Critical {
				ready = false
			}
		}
		total.got += got
		total.max += w
		g := groups[r.Group]
		if g == nil {
			g = &acc{}
			groups[r.Group] = g
		}
		g.got += got
		g.max += w
	}
	pct := func(a acc) float64 {
		if a.max == 0 {
			return 100
		}
		return math.Round(a.got/a.max*1000) / 10
	}
	out := map[string]float64{}
	for k, g := range groups {
		out[k] = pct(*g)
	}
	return pct(total), out, ready
}

func clusterInfo(ctx context.Context, t *Target) map[string]string {
	info := map[string]string{}
	if t.ManagedCluster != nil {
		labels := t.ManagedCluster.GetLabels()
		info["vendor"] = labels["vendor"]
		info["platform"] = labels["cloud"]
		info["openshift_version"] = labels["openshiftVersion"]
	}
	if t.Type == Hub {
		if m, err := t.mch(ctx); err == nil {
			info["acm_version"] = nestedStr(m.Object, "status", "currentVersion")
		}
	}
	return info
}
