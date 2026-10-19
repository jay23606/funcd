package controlplane_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const bindingsMeta = `"metadata":{"name":"x","namespace":"team-a","resourceGroup":"rg1"}`

// TestScenario_binding_kind_refused covers scenario: binding-kind-refused (ADR-0246) — a kind accepts only the
// bindings something reads: a step's bindings.links and a CatalogService's bindings.kv or bindings.sql are refused at
// apply, naming the field.
func TestScenario_binding_kind_refused(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	for name, c := range map[string]struct{ plural, body, field string }{
		"step links": {"workflows", `{"apiVersion":"funcd.io/v1alpha1","kind":"Workflow",` + bindingsMeta +
			`,"spec":{"steps":[{"name":"s","function":{"image":"oci:s","bindings":{"links":[{"alias":"a","target":"b"}]}}}]}}`, "links"},
		"catalog kv": {"catalogservices", `{"apiVersion":"funcd.io/v1alpha1","kind":"CatalogService",` + bindingsMeta +
			`,"spec":{"catalog":{"bucket":"b","prefix":"p"},"bindings":{"blob":[],"kv":[{"alias":"a","store":"s","table":"t"}]}}}`, "kv"},
		"catalog sql": {"catalogservices", `{"apiVersion":"funcd.io/v1alpha1","kind":"CatalogService",` + bindingsMeta +
			`,"spec":{"catalog":{"bucket":"b","prefix":"p"},"bindings":{"blob":[],"sql":[{"alias":"a","database":"d"}]}}}`, "sql"},
	} {
		rec := do(t, srv, http.MethodPost, "/apis/funcd.io/v1alpha1/namespaces/team-a/"+c.plural, devToken, []byte(c.body))
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%s: %s", name, rec.Body.String())
		require.Contains(t, rec.Body.String(), c.field, name)
	}
}

// TestScenario_old_field_refused covers scenario: old-field-refused (ADR-0246) — a Function's spec.links, or a spec.kv
// entry with alias, is refused by the closed schema, naming the field.
func TestScenario_old_field_refused(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	for field, spec := range map[string]string{
		"links": `"links":[{"alias":"greeter","target":"greeter"}]`,
		"kv":    `"kv":[{"alias":"c","store":"counters","table":"counts"}]`,
	} {
		body := `{"apiVersion":"funcd.io/v1alpha1","kind":"Function",` + bindingsMeta +
			`,"spec":{"runtime":"nodejs22","handler":"handle","image":"oci:x",` + spec + `}}`
		rec := do(t, srv, http.MethodPost, "/apis/funcd.io/v1alpha1/namespaces/team-a/functions", devToken, []byte(body))
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%s: %s", field, rec.Body.String())
		require.Contains(t, rec.Body.String(), field, field)
	}
}
