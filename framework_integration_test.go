package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	frameworkconfig "github.com/gradii/framework/config"
	"github.com/gradii/framework/contracts/database"
	contractsdriver "github.com/gradii/framework/contracts/database/driver"
	contractsschema "github.com/gradii/framework/contracts/database/schema"
	frameworkdb "github.com/gradii/framework/database/db"
	frameworkorm "github.com/gradii/framework/database/gorm"
	frameworkschema "github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/testing/utils"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// These deliberately different Go names map only through the explicit tags and
// TableName contract, including the insert-result primary key assignment.
type integrationRecord struct {
	RecordKey    uint64 `gorm:"column:id;primaryKey;autoIncrement" db:"id"`
	DisplayValue string `gorm:"column:display_name" db:"display_name"`
	Document     string `gorm:"column:document" db:"document"`
}

func (integrationRecord) TableName() string { return "driver_users" }

func TestFrameworkIntegration(t *testing.T) {
	dsn := os.Getenv("GRADII_TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = os.Getenv("GORAVEL_TEST_POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("set GRADII_TEST_POSTGRES_DSN to use an existing test database")
	}
	parsed, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("postgres_driver_%d", time.Now().UnixNano())
	namespace := fmt.Sprintf("gdi_%x", time.Now().UnixNano())
	prefix := "pre_"
	adminConnector, err := NewConnector(database.Config{Dsn: dsn})
	require.NoError(t, err)
	admin := sql.OpenDB(adminConnector)
	_, err = admin.Exec(`create schema "` + namespace + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(`drop schema "` + namespace + `" cascade`)
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	scopedDSN := dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		uri, err := url.Parse(dsn)
		require.NoError(t, err)
		params := uri.Query()
		params.Set("search_path", namespace+",public")
		uri.RawQuery = params.Encode()
		scopedDSN = uri.String()
	} else {
		scopedDSN += " search_path='" + namespace + ",public'"
	}
	config := frameworkconfig.NewApplication("")
	config.Add("database.default", name)
	config.Add("database.pool", map[string]any{"max_open_conns": 1, "max_idle_conns": 1})
	config.Add("database.connections."+name, map[string]any{
		"driver": "postgres", "dsn": scopedDSN, "database": parsed.Database,
		"schema": "public", "prefix": prefix, "timezone": "UTC",
		"via": func() (contractsdriver.Driver, error) { return NewPostgres(config, nil, nil, name), nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := utils.NewTestLog()
	db, err := frameworkdb.BuildDB(ctx, config, log, name, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Purge()) })

	t.Run("cold single connection transaction", func(t *testing.T) {
		tx, err := db.BeginTransaction()
		require.NoError(t, err)
		defer func() { require.NoError(t, tx.Rollback()) }()
		var rows []map[string]any
		require.NoError(t, tx.Query().SelectRaw("1 as marker").Get(&rows))
		require.Len(t, rows, 1)
		require.NoError(t, tx.Commit())
	})

	driver := NewPostgres(config, nil, nil, name)
	schema, err := frameworkschema.NewSchema(config, log, nil, driver, nil, db)
	require.NoError(t, err)
	require.NoError(t, schema.Create("driver_users", func(table contractsschema.Blueprint) {
		table.BigIncrements("id")
		table.String("display_name", 100).Collation("C")
		table.Jsonb("document").Nullable()
		table.DateTime("occurred_at", 6).Nullable()
		table.Unique("display_name").Name(prefix + "user_name_unique")
	}))
	require.NoError(t, schema.Create("driver_children", func(table contractsschema.Blueprint) {
		table.BigIncrements("id")
		table.UnsignedBigInteger("user_id")
		table.Foreign("user_id").References("id").On("driver_users").CascadeOnDelete().Name(prefix + "child_user_foreign")
	}))

	t.Run("DB CRUD and JSON compilation", func(t *testing.T) {
		result, err := db.Table("driver_users").Insert(map[string]any{"display_name": "DB 你好🌏", "document": `{"roles":["reader","writer"]}`})
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected)
		count, err := db.Table("driver_users").WhereJsonContains("document->roles", "writer").Count()
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
		result, err = db.Table("driver_users").Where("display_name", "DB 你好🌏").Update(map[string]any{"display_name": "updated DB"})
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected)
		var records []integrationRecord
		require.NoError(t, db.Table("driver_users").Select("id", "display_name", "document").Where("display_name", "updated DB").Get(&records))
		require.Len(t, records, 1)
		require.Greater(t, records[0].RecordKey, uint64(0))
		result, err = db.Table("driver_users").Where("id", records[0].RecordKey).Delete()
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected)
	})

	query, endpoint, err := frameworkorm.BuildQuery(ctx, config, name, log, nil, nil)
	require.NoError(t, err)
	require.Equal(t, prefix, endpoint.Prefix)
	writer, err := query.DB()
	require.NoError(t, err)
	require.Equal(t, 1, writer.Stats().MaxOpenConnections)
	t.Run("ORM explicit mapping and result processor", func(t *testing.T) {
		record := integrationRecord{DisplayValue: "ORM 你好🌏", Document: `{"roles":["admin"]}`}
		require.NoError(t, query.Clone().Create(&record))
		require.Greater(t, record.RecordKey, uint64(0))
		var loaded integrationRecord
		require.NoError(t, query.Clone().Where("id", record.RecordKey).First(&loaded))
		require.Equal(t, record.RecordKey, loaded.RecordKey)
		require.Equal(t, record.DisplayValue, loaded.DisplayValue)
		require.JSONEq(t, record.Document, loaded.Document)
		count, err := query.Clone().Model(&integrationRecord{}).WhereJsonContains("document->roles", "admin").Count()
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
		loaded.DisplayValue = "updated ORM"
		require.NoError(t, query.Clone().Save(&loaded))
		var refreshed integrationRecord
		require.NoError(t, query.Clone().Where("id", loaded.RecordKey).First(&refreshed))
		require.Equal(t, "updated ORM", refreshed.DisplayValue)
		result, err := query.Clone().Delete(&refreshed)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RowsAffected)
	})

	t.Run("ORM transaction uses one writer", func(t *testing.T) {
		tx, err := query.Clone().BeginTransaction()
		require.NoError(t, err)
		defer func() { require.NoError(t, tx.Rollback()) }()
		record := integrationRecord{DisplayValue: "rolled back", Document: `{"roles":[]}`}
		require.NoError(t, tx.Create(&record))
		require.Greater(t, record.RecordKey, uint64(0))
		var loaded integrationRecord
		require.NoError(t, tx.Where("id", record.RecordKey).First(&loaded))
		require.NoError(t, tx.Rollback())
		count, err := query.Clone().Model(&integrationRecord{}).Where("id", record.RecordKey).Count()
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("schema mixed alterations and metadata", func(t *testing.T) {
		require.NoError(t, schema.Table("driver_users", func(table contractsschema.Blueprint) {
			table.String("notes", 120).Nullable().Comment("round trip")
			table.Integer("name_length").StoredAs("char_length(display_name)")
		}))
		require.True(t, schema.HasColumn("driver_users", "notes"))
		require.True(t, schema.HasColumn("driver_users", "name_length"))
		columns, err := schema.GetColumns("driver_users")
		require.NoError(t, err)
		byName := make(map[string]contractsdriver.Column)
		for _, column := range columns {
			byName[column.Name] = column
		}
		require.Equal(t, "C", byName["display_name"].Collation)
		require.Equal(t, "round trip", byName["notes"].Comment)
		require.True(t, byName["notes"].Nullable)
		require.NotNil(t, byName["name_length"].Generation)
		indexes, err := schema.GetIndexes("driver_users")
		require.NoError(t, err)
		require.Len(t, indexes, 2)
		require.True(t, schema.HasIndex("driver_users", prefix+"user_name_unique"))
		keys, err := schema.GetForeignKeys("driver_children")
		require.NoError(t, err)
		require.Len(t, keys, 1)
		require.Equal(t, []string{"user_id"}, keys[0].Columns)
		require.Equal(t, prefix+"driver_users", keys[0].ForeignTable)
		require.NoError(t, schema.Table("driver_users", func(table contractsschema.Blueprint) { table.RenameColumn("notes", "remarks") }))
		require.True(t, schema.HasColumn("driver_users", "remarks"))
		require.False(t, schema.HasColumn("driver_users", "notes"))
	})

	t.Run("search path and schema transaction rollback", func(t *testing.T) {
		current, err := schema.GetCurrentSchemaName()
		require.NoError(t, err)
		require.Equal(t, namespace, current)
		err = schema.Table("driver_users", func(table contractsschema.Blueprint) {
			table.String("rollback_probe", 20).Nullable()
			table.String("display_name", 100)
		})
		require.Error(t, err)
		require.False(t, schema.HasColumn("driver_users", "rollback_probe"))
		_, err = db.Table("driver_children").Insert(map[string]any{"user_id": 888888888})
		require.Error(t, err)
	})
	version, err := db.ServerVersion()
	require.NoError(t, err)
	t.Logf("PostgreSQL server %s", version)
}
