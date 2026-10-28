package postgres

import (
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/errors"
	"github.com/spf13/cast"
	"strings"
)

var _ driver.SchemaMetadataGrammar = (*SchemaGrammar)(nil)
var _ driver.SchemaCurrentSchemas = (*SchemaGrammar)(nil)

func (*SchemaGrammar) CurrentSchemas(string) []string { return []string{"public"} }

// ResolveCurrentSchemas asks the session to expand $user and quoted names and
// exclude schemas that do not exist or are not accessible to the current role.
func (r *SchemaGrammar) ResolveCurrentSchemas(_ driver.SchemaConnectionInfo, catalog driver.SchemaCatalog) ([]string, error) {
	if catalog == nil {
		return nil, errors.SchemaCurrentNamespaceUnavailable
	}
	rows, err := catalog.Select(driver.SchemaStatement{SQL: "select unnest(current_schemas(false)) as name"})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name := cast.ToString(row["name"])
		if name == "" {
			return nil, errors.SchemaCurrentNamespaceUnavailable
		}
		names = append(names, name)
	}
	return names, nil
}
func (r *SchemaGrammar) CompileSchemaMetadata(query driver.SchemaMetadataQuery) (driver.SchemaStatement, error) {
	namespace := query.Schema
	if strings.Contains(namespace, ",") {
		namespace = ""
	}
	var sql string
	var bindings []any
	switch query.Kind {
	case "hasTable":
		sql = "select exists (select 1 from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.relname = ? and n.nspname = coalesce(nullif(?, ''), current_schema()) and c.relkind in ('r', 'p')) as exists"
		bindings = []any{query.Table, namespace}
	case "schemas":
		sql = "select nspname as name, nspname = current_schema() as \"default\" from pg_namespace where nspname <> 'information_schema' and nspname not like 'pg\\_%' order by nspname"
	case "columns":
		version, err := r.serverVersion()
		if err != nil {
			return driver.SchemaStatement{}, err
		}
		major := serverMajor(version)
		generated, identity := "'' as generated, ", "'' as identity, "
		if major >= 12 {
			generated = "a.attgenerated as generated, "
		}
		if major >= 10 {
			identity = "a.attidentity as identity, "
		}
		sql = "select a.attname as name, t.typname as type_name, format_type(a.atttypid, a.atttypmod) as type, (select tc.collcollate from pg_catalog.pg_collation tc where tc.oid = a.attcollation) as collation, not a.attnotnull as nullable, (select pg_get_expr(adbin, adrelid) from pg_attrdef where c.oid = pg_attrdef.adrelid and pg_attrdef.adnum = a.attnum) as default, " + generated + identity + "col_description(c.oid, a.attnum) as comment from pg_attribute a, pg_class c, pg_type t, pg_namespace n where c.relname = ? and n.nspname = coalesce(nullif(?, ''), current_schema()) and a.attnum > 0 and not a.attisdropped and a.attrelid = c.oid and a.atttypid = t.oid and n.oid = c.relnamespace order by a.attnum"
		bindings = []any{query.Table, namespace}
	case "indexes":
		sql = "select ic.relname as name, string_agg(a.attname, ',' order by indseq.ord) as columns, am.amname as \"type\", i.indisunique as \"unique\", i.indisprimary as \"primary\" from pg_index i join pg_class tc on tc.oid = i.indrelid join pg_namespace tn on tn.oid = tc.relnamespace join pg_class ic on ic.oid = i.indexrelid join pg_am am on am.oid = ic.relam join lateral unnest(i.indkey) with ordinality as indseq(num, ord) on true left join pg_attribute a on a.attrelid = i.indrelid and a.attnum = indseq.num where tc.relname = ? and tn.nspname = coalesce(nullif(?, ''), current_schema()) group by ic.relname, am.amname, i.indisunique, i.indisprimary"
		bindings = []any{query.Table, namespace}
	case "foreignKeys":
		sql = "select c.conname as name, string_agg(la.attname, ',' order by conseq.ord) as columns, fn.nspname as foreign_schema, fc.relname as foreign_table, string_agg(fa.attname, ',' order by conseq.ord) as foreign_columns, c.confupdtype as on_update, c.confdeltype as on_delete from pg_constraint c join pg_class tc on c.conrelid = tc.oid join pg_namespace tn on tn.oid = tc.relnamespace join pg_class fc on c.confrelid = fc.oid join pg_namespace fn on fn.oid = fc.relnamespace join lateral unnest(c.conkey) with ordinality as conseq(num, ord) on true join pg_attribute la on la.attrelid = c.conrelid and la.attnum = conseq.num join pg_attribute fa on fa.attrelid = c.confrelid and fa.attnum = c.confkey[conseq.ord] where c.contype = 'f' and tc.relname = ? and tn.nspname = coalesce(nullif(?, ''), current_schema()) group by c.conname, fn.nspname, fc.relname, c.confupdtype, c.confdeltype"
		bindings = []any{query.Table, namespace}
	case "tables", "views", "types":
		column, fallback, order := "n.nspname", "n.nspname <> 'information_schema' and n.nspname not like 'pg\\_%'", " order by n.nspname, c.relname"
		switch query.Kind {
		case "tables":
			sql = "select c.relname as name, n.nspname as schema, pg_total_relation_size(c.oid) as size, obj_description(c.oid, 'pg_class') as comment from pg_class c, pg_namespace n where c.relkind in ('r', 'p') and n.oid = c.relnamespace and "
		case "views":
			column, fallback, order = "schemaname", "schemaname <> 'information_schema' and schemaname not like 'pg\\_%'", " order by schemaname, viewname"
			sql = "select viewname as name, schemaname as schema, definition from pg_views where "
		case "types":
			sql = "select t.typname as name, n.nspname as schema, t.typtype as type, t.typcategory as category, ((t.typinput = 'array_in'::regproc and t.typoutput = 'array_out'::regproc) or t.typtype = 'm') as implicit from pg_type t join pg_namespace n on n.oid = t.typnamespace left join pg_class c on c.oid = t.typrelid left join pg_type el on el.oid = t.typelem left join pg_class ce on ce.oid = el.typrelid where ((t.typrelid = 0 and (ce.relkind = 'c' or ce.relkind is null)) or c.relkind = 'c') and not exists (select 1 from pg_depend d where d.objid in (t.oid, t.typelem) and d.deptype = 'e') and "
			order = ""
		}
		filter, values := schemaPredicate(column, query.Schemas, fallback)
		sql += filter + order
		bindings = values
	default:
		return driver.SchemaStatement{}, errors.SchemaMetadataNotSupported.Args(query.Kind, Name)
	}
	return driver.SchemaStatement{SQL: sql, Bindings: bindings}, nil
}
func schemaPredicate(column string, schemas []string, fallback string) (string, []any) {
	if len(schemas) == 0 || (len(schemas) == 1 && schemas[0] == "") {
		return fallback, nil
	}
	values := make([]any, len(schemas))
	for i, schema := range schemas {
		values[i] = schema
	}
	if len(schemas) == 1 {
		return column + " = ?", values
	}
	return column + " in (" + strings.TrimSuffix(strings.Repeat("?, ", len(schemas)), ", ") + ")", values
}
