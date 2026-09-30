package checks

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const labelRootPolicy = "policy.open-cluster-management.io/root-policy"

// boundPolicies are the replicated policies in the cluster's namespace on the hub, excluding
// the fleet-validator probe policy (its results feed the Platform group instead).
func (t *Target) boundPolicies(ctx context.Context) ([]unstructured.Unstructured, error) {
	items, err := t.list(ctx, gvrPolicy, t.Name)
	if err != nil {
		return nil, err
	}
	out := make([]unstructured.Unstructured, 0, len(items))
	for _, p := range items {
		root, ok := p.GetLabels()[labelRootPolicy]
		if ok && root != t.Env.Cfg.ProbePolicy {
			out = append(out, p)
		}
	}
	return out, nil
}

func templateKinds(p *unstructured.Unstructured) map[string]int {
	out := map[string]int{}
	tmpls, _, _ := unstructured.NestedSlice(p.Object, "spec", "policy-templates")
	for _, raw := range tmpls {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		od, _ := m["objectDefinition"].(map[string]any)
		out[str(od["kind"])]++
	}
	return out
}

func checkPoliciesBound(ctx context.Context, t *Target) (State, string) {
	ps, err := t.boundPolicies(ctx)
	if err != nil {
		return skipOrFail(err, "Policies")
	}
	if len(ps) == 0 {
		return Fail, "no policies placed on " + t.Name
	}
	return Pass, fmt.Sprintf("%d policies placed", len(ps))
}

func checkPolicyKindCount(kind string) func(context.Context, *Target) (State, string) {
	return func(ctx context.Context, t *Target) (State, string) {
		ps, err := t.boundPolicies(ctx)
		if err != nil {
			return skipOrFail(err, "Policies")
		}
		g := t.Governance()
		want := g.MinConfigurationPolicies
		if kind == "OperatorPolicy" {
			want = g.MinOperatorPolicies
		}
		have, inPolicies := 0, 0
		for i := range ps {
			if n := templateKinds(&ps[i])[kind]; n > 0 {
				have += n
				inPolicies++
			}
		}
		d := fmt.Sprintf("%d %s(s) in %d policies (want >= %d)", have, kind, inPolicies, want)
		if have < want {
			return Fail, d
		}
		return Pass, d
	}
}

func checkPoliciesCompliant(ctx context.Context, t *Target) (State, string) {
	ps, err := t.boundPolicies(ctx)
	if err != nil {
		return skipOrFail(err, "Policies")
	}
	if len(ps) == 0 {
		return Skip, "no policies placed"
	}
	var nonCompliant, pending []string
	for _, p := range ps {
		root := p.GetLabels()[labelRootPolicy]
		switch nestedStr(p.Object, "status", "compliant") {
		case "Compliant":
		case "NonCompliant":
			nonCompliant = append(nonCompliant, root)
		default:
			pending = append(pending, root)
		}
	}
	if len(nonCompliant) > 0 {
		return Fail, fmt.Sprintf("%d/%d NonCompliant: %s", len(nonCompliant), len(ps), names(nonCompliant, 4))
	}
	if len(pending) > 0 {
		return Warn, fmt.Sprintf("%d/%d pending: %s", len(pending), len(ps), names(pending, 4))
	}
	return Pass, fmt.Sprintf("%d/%d Compliant", len(ps), len(ps))
}
