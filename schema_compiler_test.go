package postgres

import (
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
)

func TestSchemaMixedColumns(t *testing.T) {
	g := NewSchemaGrammar("pre_")
	g.SetVersionResolver(func() (string, error) { return "18.0", nil })
	blueprint := schema.NewBlueprint(nil, "pre_", "items")
	blueprint.Create()
	blueprint.BigIncrements("id")
	blueprint.String("name", 80).Collation("C")
	blueprint.Binary("payload")
	blueprint.IpAddress("ip")
	blueprint.MacAddress("mac")
	blueprint.Integer("size").StoredAs("char_length(name)")
	blueprint.Integer("virtual_size").VirtualAs("char_length(name)")
	statements, err := blueprint.CompileStatements(g)
	require.NoError(t, err)
	sql := statements[0].SQL
	for _, fragment := range []string{`create table "pre_items"`, `"id" bigserial`, `"name" varchar(80) collate "C"`, `"payload" bytea`, `"ip" inet`, `"mac" macaddr`, `generated always as (char_length(name)) stored`, `generated always as (char_length(name)) virtual`} {
		require.Contains(t, sql, fragment)
	}
}
func TestSchemaGeneratedVersionBoundaries(t *testing.T) {
	for _, test := range []struct {
		version string
		stored  bool
		valid   bool
	}{{"11.22", true, false}, {"12.0", true, true}, {"17.5", false, false}, {"18.0", false, true}} {
		g := NewSchemaGrammar("")
		g.SetVersionResolver(func() (string, error) { return test.version, nil })
		blueprint := schema.NewBlueprint(nil, "", "items")
		blueprint.Create()
		column := blueprint.Integer("n")
		if test.stored {
			column.StoredAs("1")
		} else {
			column.VirtualAs("1")
		}
		_, err := blueprint.CompileStatements(g)
		if test.valid {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, errors.SchemaFeatureNotSupported)
		}
	}
}
func TestSchemaMetadataVersionAndBindings(t *testing.T) {
	g := NewSchemaGrammar("")
	for _, version := range []string{"9.6.24", "11.22", "12.0", "18.0"} {
		g.SetVersionResolver(func() (string, error) { return version, nil })
		statement, err := g.CompileSchemaMetadata(driver.SchemaMetadataQuery{Kind: "columns", Schema: "odd'schema", Table: "items"})
		require.NoError(t, err)
		require.Equal(t, []any{"items", "odd'schema"}, statement.Bindings)
		require.NotContains(t, statement.SQL, "odd'schema")
		require.Equal(t, serverMajor(version) >= 12, strings.Contains(statement.SQL, "a.attgenerated"))
		require.Equal(t, serverMajor(version) >= 10, strings.Contains(statement.SQL, "a.attidentity"))
	}
	g.SetVersionResolver(func() (string, error) { return "", io.ErrUnexpectedEOF })
	_, err := g.CompileSchemaMetadata(driver.SchemaMetadataQuery{Kind: "columns"})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
func TestSchemaSearchPathAndOperations(t *testing.T) {
	g := NewSchemaGrammar("")
	names, err := g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{Username: "app", SearchPath: `"$user", public`}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"app", "public"}, names)
	statements, err := g.CompileSchemaOperation(driver.SchemaOperation{Kind: "dropAllTypes", Types: []driver.Type{{Name: "enum_name", Schema: "tenant", Type: "enum"}, {Name: "domain_name", Schema: "tenant", Type: "domain"}, {Name: "_array", Schema: "tenant", Implicit: true}}})
	require.NoError(t, err)
	require.Equal(t, []driver.SchemaStatement{{SQL: `drop type "tenant"."enum_name" cascade`}, {SQL: `drop domain "tenant"."domain_name" cascade`}}, statements)
	require.True(t, g.SchemaExecutionPolicy(nil).Transactional)
	require.True(t, g.SchemaOperationPolicy(driver.SchemaOperation{Kind: "dropAllTables"}).Transactional)
	require.False(t, g.SchemaOperationPolicy(driver.SchemaOperation{Kind: "createDatabase"}).Transactional)
}
