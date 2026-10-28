package postgres

import (
	"context"
	"database/sql/driver"
	"github.com/gradii/framework/contracts/database"
	"github.com/gradii/framework/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"testing"
)

type testConnector struct{}

func (*testConnector) Connect(context.Context) (driver.Conn, error) { return nil, driver.ErrBadConn }
func (*testConnector) Driver() driver.Driver                        { return &stdlib.Driver{} }
func TestConnectorConfiguration(t *testing.T) {
	endpoint := database.Config{Host: "2001:db8::1", Port: 5433, Username: "user@name", Password: "p@ss:/?#&", Database: "db/name ?", Schema: "tenant, public", Timezone: "Asia/Shanghai", Sslmode: "disable"}
	config, err := connectorConfig(endpoint)
	require.NoError(t, err)
	require.Equal(t, endpoint.Host, config.Host)
	require.Equal(t, uint16(endpoint.Port), config.Port)
	require.Equal(t, endpoint.Username, config.User)
	require.Equal(t, endpoint.Password, config.Password)
	require.Equal(t, endpoint.Database, config.Database)
	require.Equal(t, endpoint.Schema, config.RuntimeParams["search_path"])
	require.Equal(t, endpoint.Timezone, config.RuntimeParams["timezone"])
	require.Equal(t, pgx.QueryExecModeSimpleProtocol, config.DefaultQueryExecMode)
	require.Nil(t, config.TLSConfig)
	connector, err := NewConnector(endpoint)
	require.NoError(t, err)
	require.NotNil(t, connector)
}
func TestConnectorNativeDSNPrecedence(t *testing.T) {
	config, err := connectorConfig(database.Config{Dsn: "host=127.0.0.1 port=5434 user=native password='quoted password' dbname=native sslmode=disable search_path='a,b' timezone=Europe/Paris application_name=gradii default_query_exec_mode=cache_statement", Host: "ignored", Port: -1, Database: "ignored", Schema: "ignored", Timezone: "ignored", Sslmode: "invalid"})
	require.NoError(t, err)
	require.Equal(t, "native", config.Database)
	require.Equal(t, "quoted password", config.Password)
	require.Equal(t, "a,b", config.RuntimeParams["search_path"])
	require.Equal(t, "Europe/Paris", config.RuntimeParams["timezone"])
	require.Equal(t, "gradii", config.RuntimeParams["application_name"])
	require.Equal(t, pgx.QueryExecModeSimpleProtocol, config.DefaultQueryExecMode)
}
func TestConnectorValidationAndOverride(t *testing.T) {
	for _, endpoint := range []database.Config{{}, {Host: "localhost", Port: -1}, {Host: "localhost", Port: 65536}, {Host: "localhost", Sslmode: "invalid"}, {Dsn: "not a dsn"}} {
		connector, err := NewConnector(endpoint)
		require.Error(t, err)
		require.Nil(t, connector)
	}
	_, err := NewConnector(database.Config{})
	require.ErrorIs(t, err, errors.DatabaseConfigNotFound)
	custom := &testConnector{}
	connector, err := NewConnector(database.Config{Connector: custom, Dsn: "not a dsn"})
	require.NoError(t, err)
	require.Same(t, custom, connector)
}
func TestPoolDefersValidationAndDisablesNamingInference(t *testing.T) {
	postgres := NewPostgres(configValues{values: map[string]any{"database.connections.pg.dsn": "bad dsn", "database.connections.pg.prefix": "pre_", "database.connections.pg.singular": true, "database.connections.pg.no_lower_case": true}}, nil, nil, "pg")
	pool := postgres.Pool()
	require.Len(t, pool.Writers, 1)
	require.Equal(t, "bad dsn", pool.Writers[0].Dsn)
	require.Equal(t, "pre_", pool.Writers[0].Prefix)
	require.Empty(t, pool.Writers[0].Schema)
	require.False(t, pool.Writers[0].Singular)
	require.False(t, pool.Writers[0].NoLowerCase)
	require.Nil(t, pool.Writers[0].NameReplacer)
	require.NotNil(t, postgres.Grammar())
	require.Equal(t, Name, postgres.Dialect().Name())
}
