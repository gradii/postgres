package postgres

import (
	"testing"

	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"github.com/stretchr/testify/require"
)

type enumTestCatalog struct {
	rows []map[string]any
	err  error
}

func TestEmptyEnumValuesAreRejectedBeforeDDL(t *testing.T) {
	b := schema.NewBlueprint(nil, "", "items")
	b.Create()
	b.Enum("state", nil)
	statements, err := b.CompileStatements(NewSchemaGrammar(""))
	require.ErrorIs(t, err, errors.SchemaEnumValuesRequired)
	require.Empty(t, statements)
}

func (c enumTestCatalog) Select(driver.SchemaStatement) ([]map[string]any, error) {
	return c.rows, c.err
}
func (c enumTestCatalog) Columns(string) ([]driver.ColumnMetadata, error) { return nil, nil }
func (c enumTestCatalog) ResolveTable(string) (string, string, error)     { return "tenant", "items", nil }

func TestEnumMembershipRecognition(t *testing.T) {
	for _, test := range []struct {
		expression string
		valid      bool
	}{
		{"state in ('a', 'b')", true},
		{"((state)::text = ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[]))", true},
		{"((state = 'a') OR (state = 'b'))", true},
		{"state in ('O''Reilly', '', '中文')", true},
		{"state in ('a') OR 1 = 1", false},
		{"lower(state) in ('a')", false},
		{"state in ('a') AND other = 'b'", false},
		{"state NOT IN ('a')", false},
		{"other in ('a')", false},
		{"state::integer = '1'", false},
		{"state::varchar(1) in ('a')", false},
		{"state::bpchar in ('a')", false},
		{"state in ('unterminated)", false},
	} {
		t.Run(test.expression, func(t *testing.T) { require.Equal(t, test.valid, enumMembership(test.expression, "state")) })
	}
}

func TestEnumChangePlansOwnedConstraints(t *testing.T) {
	g := NewSchemaGrammar("")
	b := schema.NewBlueprint(nil, "", "tenant.items")
	b.Enum("state", []any{"a", "b", "c"}).Nullable().Change()
	rows := []map[string]any{
		{"name": "items_state_check", "definition": "state in ('a', 'b')", "single_column": true},
		{"name": "custom_length", "definition": "length(state) < 20", "single_column": true},
	}
	statements, handled, err := g.CompileSchemaBlueprint(b, enumTestCatalog{rows: rows})
	require.True(t, handled)
	require.NoError(t, err)
	require.Contains(t, statements[0].SQL, "drop constraint")
	require.Contains(t, statements[1].SQL, "type varchar(255)")
	require.NotContains(t, statements[1].SQL, "check")
	require.Contains(t, statements[2].SQL, "add constraint")
	require.Contains(t, statements[2].SQL, "'c'")
	for _, statement := range statements {
		require.NotContains(t, statement.SQL, "custom_length")
	}
}

func TestEnumChangeRejectsAmbiguousConstraintsBeforeCompiling(t *testing.T) {
	for _, rows := range [][]map[string]any{
		{{"name": "custom_membership", "definition": "state in ('a')", "single_column": true}},
		{{"name": "items_state_check", "definition": "state in ('a')", "single_column": true}, {"name": "items_state_check1", "definition": "state in ('a')", "single_column": true}},
		{{"name": "items_state_check", "definition": "lower(state) in ('a')", "single_column": true}},
	} {
		g := NewSchemaGrammar("")
		b := schema.NewBlueprint(nil, "", "tenant.items")
		b.Enum("state", []any{"a", "b"}).Change()
		statements, handled, err := g.CompileSchemaBlueprint(b, enumTestCatalog{rows: rows})
		require.True(t, handled)
		require.ErrorIs(t, err, errors.SchemaEnumConstraintAmbiguous)
		require.Empty(t, statements)
	}
	b := schema.NewBlueprint(nil, "", "items")
	b.Enum("state", []any{"a"}).Change()
	_, err := b.CompileStatements(NewSchemaGrammar(""))
	require.ErrorIs(t, err, errors.SchemaEnumCatalogRequired)
}

func TestGeneratedExpressionRemovalPrecedesDefault(t *testing.T) {
	for _, withDefault := range []bool{false, true} {
		g := NewSchemaGrammar("")
		g.SetVersionResolver(func() (string, error) { return "18.3", nil })
		b := schema.NewBlueprint(nil, "", "items")
		c := b.Integer("n").StoredAs(nil).Change()
		if withDefault {
			c.Default(7)
		}
		statements, err := b.CompileStatements(g)
		require.NoError(t, err)
		require.Contains(t, statements[0].SQL, "alter column \"n\" drop expression if exists, alter column \"n\" type integer")
	}
	g := NewSchemaGrammar("")
	g.SetVersionResolver(func() (string, error) { return "12.22", nil })
	b := schema.NewBlueprint(nil, "", "items")
	b.Integer("n").StoredAs(nil).Change()
	_, err := b.CompileStatements(g)
	require.ErrorIs(t, err, errors.SchemaFeatureNotSupported)
}
