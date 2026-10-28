package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	frameworkconfig "github.com/gradii/framework/config"
	contractsdb "github.com/gradii/framework/contracts/database/db"
	contractsdriver "github.com/gradii/framework/contracts/database/driver"
	contractsschema "github.com/gradii/framework/contracts/database/schema"
	frameworkdb "github.com/gradii/framework/database/db"
	frameworkschema "github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"github.com/gradii/framework/testing/utils"
	"github.com/stretchr/testify/require"
)

func ddlIntegration(t *testing.T) (contractsschema.Schema, contractsdb.DB, string) {
	t.Helper()
	dsn := os.Getenv("GRADII_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set GRADII_TEST_POSTGRES_DSN to exercise native schema changes")
	}
	name := fmt.Sprintf("ddl_postgres_%x", time.Now().UnixNano())
	namespace := fmt.Sprintf("ddl_%x", time.Now().UnixNano())
	config := frameworkconfig.NewApplication("")
	config.Add("database.default", name)
	config.Add("database.pool", map[string]any{"max_open_conns": 1, "max_idle_conns": 1})
	config.Add("database.connections."+name, map[string]any{
		"driver": "postgres", "dsn": dsn,
		"via": func() (contractsdriver.Driver, error) { return NewPostgres(config, nil, nil, name), nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	log := utils.NewTestLog()
	db, err := frameworkdb.BuildDB(ctx, config, log, name, nil)
	require.NoError(t, err)
	require.NoError(t, db.Statement("create schema "+namespace))
	t.Cleanup(func() {
		require.NoError(t, db.StatementContext(context.Background(), "drop schema "+namespace+" cascade"))
		require.NoError(t, db.Purge())
	})
	s, err := frameworkschema.NewSchema(config, log, nil, NewPostgres(config, nil, nil, name), nil, db)
	require.NoError(t, err)
	return s, db, namespace
}

func TestSchemaDDLIntegration(t *testing.T) {
	s, db, namespace := ddlIntegration(t)
	table := func(name string) string { return namespace + "." + name }
	cleanup := func(t *testing.T, name string) {
		t.Helper()
		t.Cleanup(func() { require.NoError(t, s.DropIfExists(table(name))) })
	}
	t.Run("enum change preserves other checks and rolls back invalid restrictions", func(t *testing.T) {
		name := table("enum_items")
		cleanup(t, "enum_items")
		require.NoError(t, s.Create(name, func(b contractsschema.Blueprint) {
			b.Integer("id")
			b.Enum("state", []any{"open", "closed", ""}).Nullable().Default("open").Comment("old state")
			b.String("note", 30).Nullable()
			b.Index("state").Name("enum_state_idx")
		}))
		require.NoError(t, db.Statement("alter table "+name+" add constraint extra_length check (char_length(state) < 20)"))
		require.NoError(t, db.Statement("insert into "+name+" (id, state) values (?, ?)", 1, "open"))
		require.NoError(t, s.Table(name, func(b contractsschema.Blueprint) {
			b.Enum("state", []any{"open", "closed", "", "new", "O'Reilly", "中文"}).Nullable().Default("new").Comment("new state").Change()
			b.String("note", 50).Nullable().Change()
		}))
		for i, value := range []string{"new", "O'Reilly", "中文", ""} {
			require.NoError(t, db.Statement("insert into "+name+" (id, state) values (?, ?)", i+2, value))
		}
		require.NoError(t, db.Statement("insert into "+name+" (id) values (?)", 20))
		var rows []map[string]any
		require.NoError(t, db.Select(&rows, "select state from "+name+" where id = 20"))
		require.Equal(t, "new", fmt.Sprint(rows[0]["state"]))
		require.Error(t, db.Statement("insert into "+name+" (id, state) values (?, ?)", 90, "invalid"))
		require.Error(t, s.Table(name, func(b contractsschema.Blueprint) {
			b.Enum("state", []any{"closed"}).Nullable().Default("closed").Change()
			b.String("note", 80).Nullable().Change()
		}))
		// A failed restriction restores the previous membership and default.
		require.NoError(t, db.Statement("insert into "+name+" (id, state) values (?, ?)", 91, "new"))
		require.NoError(t, db.Statement("insert into "+name+" (id) values (?)", 92))
		rows = nil
		require.NoError(t, db.Select(&rows, "select state from "+name+" where id = 92"))
		require.Equal(t, "new", fmt.Sprint(rows[0]["state"]))
		indexes, err := s.GetIndexes(name)
		require.NoError(t, err)
		require.Len(t, indexes, 1)
		require.Equal(t, "enum_state_idx", indexes[0].Name)
		// Expand to a value disallowed by the independent CHECK; it must survive.
		longValue := "this_label_is_longer_than_twenty"
		require.NoError(t, s.Table(name, func(b contractsschema.Blueprint) {
			b.Enum("state", []any{"open", "closed", "", "new", "O'Reilly", "中文", longValue}).Nullable().Change()
		}))
		require.Error(t, db.Statement("insert into "+name+" (id, state) values (?, ?)", 93, longValue))
	})
	t.Run("legacy enum check and custom membership boundary", func(t *testing.T) {
		name := table("legacy_items")
		cleanup(t, "legacy_items")
		require.NoError(t, db.Statement("create table "+name+" (state varchar(255) check (state in ('a', 'b')) not null)"))
		require.NoError(t, db.Statement("alter table "+name+" add check (char_length(state) < 20)"))
		require.NoError(t, s.Table(name, func(b contractsschema.Blueprint) { b.Enum("state", []any{"a", "b", "c"}).Change() }))
		require.NoError(t, db.Statement("insert into "+name+" (state) values (?)", "c"))

		custom := table("custom_items")
		cleanup(t, "custom_items")
		require.NoError(t, db.Statement("create table "+custom+" (state varchar(255) not null, constraint custom_membership check (state in ('a', 'b')))"))
		err := s.Table(custom, func(b contractsschema.Blueprint) { b.Enum("state", []any{"a", "b", "c"}).Change() })
		require.ErrorIs(t, err, errors.SchemaEnumConstraintAmbiguous)
		require.NoError(t, db.Statement("insert into "+custom+" (state) values (?)", "a"))
		require.Error(t, db.Statement("insert into "+custom+" (state) values (?)", "c"))
	})
	t.Run("stored generated expression becomes an ordinary column", func(t *testing.T) {
		name := table("generated_items")
		cleanup(t, "generated_items")
		require.NoError(t, s.Create(name, func(b contractsschema.Blueprint) {
			b.Integer("source").Nullable()
			b.Integer("doubled").StoredAs("source * 2")
			b.Integer("optional").StoredAs("source * 3").Nullable()
		}))
		require.NoError(t, db.Statement("insert into "+name+" (source) values (?)", 3))
		require.NoError(t, s.Table(name, func(b contractsschema.Blueprint) {
			b.Integer("doubled").StoredAs(nil).Default(7).Change()
			b.Integer("optional").StoredAs(nil).Nullable().Change()
		}))
		var rows []map[string]any
		require.NoError(t, db.Select(&rows, "select doubled, optional from "+name))
		require.Equal(t, "6", fmt.Sprint(rows[0]["doubled"]))
		require.Equal(t, "9", fmt.Sprint(rows[0]["optional"]))
		require.NoError(t, db.Statement("insert into "+name+" (source) values (?)", 2))
		rows = nil
		require.NoError(t, db.Select(&rows, "select doubled, optional from "+name+" where source = 2"))
		require.Equal(t, "7", fmt.Sprint(rows[0]["doubled"]))
		require.Nil(t, rows[0]["optional"])
		require.NoError(t, db.Statement("update "+name+" set doubled = 19 where source = 2"))
		columns, err := s.GetColumnsMetadata(name)
		require.NoError(t, err)
		for _, column := range columns {
			require.Nil(t, column.Generation)
		}
	})
}
