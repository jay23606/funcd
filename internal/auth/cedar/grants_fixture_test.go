package cedar

import v1 "github.com/pyvvo/funcd/api/types/v1alpha1"

// grantFixtures are the principals the grants-unchanged golden covers (ADR-0246): a Function with one binding of every
// kind, and a CatalogService with its blob binding. Only how they are written may change; the golden may not.
func grantFixtures() map[string]PrincipalObject {
	fn := &v1.Function{TypeMeta: v1.TypeMeta{APIVersion: v1.KindFunction.GVK().APIVersion(), Kind: v1.KindFunction}}
	fn.Name, fn.Namespace = "api", "team"
	fn.Spec.KV = []v1.FunctionKV{{Alias: "counters", Store: "counters", Table: "counts"}, {Alias: "cache", Store: "cache", Table: "hot"}}
	fn.Spec.Blob = []v1.FunctionBlob{{Alias: "files", Bucket: "files", Prefix: "uploads"}}
	fn.Spec.SQL = []v1.FunctionSQL{{Alias: "db", Database: "todo-db"}}
	fn.Spec.Catalogs = []v1.FunctionCatalog{{Alias: "lake", Catalog: "lake"}}
	fn.Spec.Links = []v1.FunctionLink{{Alias: "billing", Target: "billing"}}
	fn.Spec.Config = []v1.ObjectName{"settings"}
	fn.Spec.Secrets = []v1.ObjectName{"api-keys"}

	cs := &v1.CatalogService{TypeMeta: v1.TypeMeta{APIVersion: v1.KindCatalogService.GVK().APIVersion(), Kind: v1.KindCatalogService}}
	cs.Name, cs.Namespace = "lake", "team"
	cs.Spec.Blob = []v1.FunctionBlob{{Alias: "catalog", Bucket: "lakehouse", Prefix: "gold"}, {Alias: "raw", Bucket: "lakehouse", Prefix: "bronze"}}
	cs.Spec.Config = []v1.ObjectName{"engine-config"}
	cs.Spec.Secrets = []v1.ObjectName{"quack-token"}
	return map[string]PrincipalObject{"Function": fn, "CatalogService": cs}
}
