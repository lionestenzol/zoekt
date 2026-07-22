package ctags

import (
	"os"
	"testing"
)

// TestScipBatchCockpitDoD is the acceptance test for the scip-ctags Windows
// wiring: scip-ctags must tag all 15 exported functions of delta-kernel's
// cockpit.ts, where universal-ctags batch mode tagged only a few. Skips unless
// SCIP_CTAGS_TEST_BIN (and, for the baseline, CTAGS_TEST_BIN) point at real
// binaries and COCKPIT_TS points at the source file - it is a machine-local
// verification, not a portable unit test.
func TestScipBatchCockpitDoD(t *testing.T) {
	scipBin := os.Getenv("SCIP_CTAGS_TEST_BIN")
	src := os.Getenv("COCKPIT_TS")
	if scipBin == "" || src == "" {
		t.Skip("set SCIP_CTAGS_TEST_BIN and COCKPIT_TS to run the DoD check")
	}
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}

	want := []string{
		"isActionAllowedInMode", "getEffectiveRiskTier", "buildCockpit",
		"createPendingAction", "buildConfirmPatch", "buildCancelPatch",
		"buildExpirePatch", "confirmPendingAction", "cancelPendingAction",
		"expirePendingAction", "createDraft", "buildDraftAppliedPatch",
		"applyDraft", "getAffectedSections", "shouldRerender",
	}

	scip := newScipBatchParser(scipBin)
	entries, err := scip.Parse(src, content)
	if err != nil {
		t.Fatalf("scip parse: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name] = true
	}
	missing := []string{}
	for _, w := range want {
		if !got[w] {
			missing = append(missing, w)
		}
	}
	t.Logf("scip-ctags: %d total entries, %d/%d target exported funcs", len(entries), len(want)-len(missing), len(want))

	// Baseline for comparison, if universal-ctags is provided.
	if uniBin := os.Getenv("CTAGS_TEST_BIN"); uniBin != "" {
		uni := newBatchParser(uniBin)
		ue, err := uni.Parse(src, content)
		if err == nil {
			ug := map[string]bool{}
			for _, e := range ue {
				ug[e.Name] = true
			}
			hit := 0
			for _, w := range want {
				if ug[w] {
					hit++
				}
			}
			t.Logf("universal-ctags (baseline): %d total entries, %d/%d target exported funcs", len(ue), hit, len(want))
		}
	}

	if len(missing) > 0 {
		t.Errorf("scip-ctags missed %d/%d exported funcs: %v", len(missing), len(want), missing)
	}
}
