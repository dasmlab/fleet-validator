package checks

import "testing"

func TestScore(t *testing.T) {
	res := []Result{
		{Group: "A", Severity: Critical, State: Pass},
		{Group: "A", Severity: Warning, State: Warn},
		{Group: "B", Severity: Warning, State: Fail},
		{Group: "B", Severity: Critical, State: Skip},
	}
	total, groups, ready := score(res)
	// got = 5 + 1 + 0 = 6 of max 5 + 2 + 2 = 9
	if total != 66.7 {
		t.Errorf("total = %v", total)
	}
	if groups["A"] != 85.7 || groups["B"] != 0 {
		t.Errorf("groups = %v", groups)
	}
	if !ready {
		t.Error("no critical failure, want ready")
	}

	_, _, ready = score([]Result{{Group: "A", Severity: Critical, State: Fail}})
	if ready {
		t.Error("critical failure, want not ready")
	}
	if total, _, _ := score(nil); total != 100 {
		t.Errorf("empty = %v", total)
	}
}

func TestCatalogIDsUnique(t *testing.T) {
	for typ, list := range Catalog() {
		seen := map[string]bool{}
		for _, c := range list {
			if seen[c.ID] {
				t.Errorf("%s: duplicate id %s", typ, c.ID)
			}
			seen[c.ID] = true
			if c.Run == nil || c.Group == "" || c.Title == "" {
				t.Errorf("%s: incomplete check %s", typ, c.ID)
			}
		}
	}
}
