package cedar

import (
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

//nolint:gochecknoglobals // the standard -update flag of a golden test
var updateGolden = flag.Bool("update", false, "rewrite testdata/grants.golden.json")

// TestScenario_grants_unchanged covers scenario: grants-unchanged (ADR-0246) — every capability's binding-as-grant
// attribute on each fixture principal equals the golden recorded before bindings moved under spec.bindings.
func TestScenario_grants_unchanged(t *testing.T) {
	got := map[string]map[string][]string{}
	for kind, obj := range grantFixtures() {
		attrs := map[string][]string{}
		for _, c := range defaultRegistry.caps {
			if c.PrincipalBinding.Bind == nil {
				continue
			}
			uids := []string{}
			for _, u := range c.PrincipalBinding.Bind("team", obj) {
				uids = append(uids, u.String())
			}
			slices.Sort(uids)
			attrs[c.PrincipalBinding.Attr] = uids
		}
		got[kind] = attrs
	}
	b, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	b = append(b, '\n')
	const golden = "testdata/grants.golden.json"
	if *updateGolden {
		require.NoError(t, os.WriteFile(golden, b, 0o600))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	require.Equal(t, string(want), string(b))
}
