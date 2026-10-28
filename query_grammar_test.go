package postgres

import (
	contractsquery "github.com/gradii/framework/contracts/database/query"
	querybuilder "github.com/gradii/framework/database/query"
	"github.com/gradii/framework/database/query/core"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQueryGrammarPostgresSQL(t *testing.T) {
	for _, test := range []struct {
		name     string
		build    func(*querybuilder.Builder) *querybuilder.Builder
		sql      string
		bindings []any
	}{
		{"JSON time", func(b *querybuilder.Builder) *querybuilder.Builder {
			return b.WhereTime("payload->time", ">=", "22:00")
		}, `select * from "pre_users" where ("payload"->>'time')::time >= ?`, []any{"22:00"}},
		{"JSON exists operator", func(b *querybuilder.Builder) *querybuilder.Builder { return b.Where("payload", "?", "name") }, `select * from "pre_users" where "payload" ?? ?`, []any{"name"}},
		{"case insensitive like", func(b *querybuilder.Builder) *querybuilder.Builder {
			return b.WhereLike("name", "a%", false, core.BooleanAnd, false)
		}, `select * from "pre_users" where "name"::text ilike ?`, []any{"a%"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := test.build(querybuilder.NewBuilder(NewQueryGrammar("pre_")).From("users", ""))
			require.Equal(t, test.sql, b.ToSQL())
			require.Equal(t, test.bindings, b.GetBindings())
		})
	}
	b := querybuilder.NewBuilder(NewQueryGrammar("pre_")).From("users", "")
	sql, bindings := b.ToInsertGetIdSQL([]map[string]any{{"name": "a"}}, "user_id")
	require.Equal(t, `insert into "pre_users" ("name") values (?) returning "user_id"`, sql)
	require.Equal(t, []any{"a"}, bindings)
}
func TestPostgresQueryProcessorMetadata(t *testing.T) {
	processor := NewQueryProcessor()
	columns := processor.ProcessColumns([]contractsquery.Row{{"name": "identity", "identity": "a", "nullable": false}, {"name": "stored", "generated": "s", "default": "length(name)", "nullable": true}, {"name": "virtual", "generated": "v", "default": "length(name)"}})
	require.Equal(t, true, columns[0]["auto_increment"])
	require.Nil(t, columns[1]["default"])
	require.Equal(t, contractsquery.Row{"type": "stored", "expression": "length(name)"}, columns[1]["generation"])
	require.Equal(t, contractsquery.Row{"type": "virtual", "expression": "length(name)"}, columns[2]["generation"])
}
func TestPostgresLiteralEscaping(t *testing.T) {
	for _, test := range []struct {
		value  any
		binary bool
		want   string
	}{{true, false, "true"}, {false, false, "false"}, {[]byte{0, 255}, true, `'\x00ff'::bytea`}, {`a\b'c`, false, `'a\b''c'`}} {
		sql, err := NewDialect().Escape(test.value, test.binary)
		require.NoError(t, err)
		require.Equal(t, test.want, sql)
	}
}

func TestPostgresRawSQLRestoresJSONOperators(t *testing.T) {
	require.Equal(t, `select * from "users" where "roles" ? 'admin'`,
		NewQueryGrammar("").SubstituteBindingsIntoRawSQL(`select * from "users" where "roles" ?? ?`, []any{"admin"}))
}
