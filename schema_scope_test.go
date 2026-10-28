package postgres

import (
	"context"
	"testing"

	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/errors"
	"github.com/stretchr/testify/require"
)

type currentSchemaCatalog struct {
	driver.SchemaCatalog
	rows      []map[string]any
	err       error
	statement driver.SchemaStatement
}

func (c *currentSchemaCatalog) Select(statement driver.SchemaStatement) ([]map[string]any, error) {
	c.statement = statement
	return c.rows, c.err
}

func TestCurrentSchemasUseServerSearchPath(t *testing.T) {
	g := NewSchemaGrammar("")
	catalog := &currentSchemaCatalog{rows: []map[string]any{{"name": "actual_user"}, {"name": "tenant,quoted"}}}
	names, err := g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{Username: "stale_user", Schema: "public", SearchPath: `"$user", public`}, catalog)
	require.NoError(t, err)
	require.Equal(t, []string{"actual_user", "tenant,quoted"}, names)
	require.Equal(t, driver.SchemaStatement{SQL: "select unnest(current_schemas(false)) as name"}, catalog.statement)
	names, err = g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{Schema: "public"}, &currentSchemaCatalog{})
	require.NoError(t, err)
	require.Empty(t, names, "an empty effective search path must not fall back to public")
	for _, rows := range [][]map[string]any{{{"name": nil}}, {{"name": ""}}} {
		_, err = g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{}, &currentSchemaCatalog{rows: rows})
		require.ErrorIs(t, err, errors.SchemaCurrentNamespaceUnavailable)
	}
	_, err = g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{}, nil)
	require.ErrorIs(t, err, errors.SchemaCurrentNamespaceUnavailable)
	_, err = g.ResolveCurrentSchemas(driver.SchemaConnectionInfo{}, &currentSchemaCatalog{err: context.Canceled})
	require.ErrorIs(t, err, context.Canceled)
}
