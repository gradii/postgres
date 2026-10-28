package postgres

import (
	contractsconfig "github.com/gradii/framework/contracts/config"
	querybuilder "github.com/gradii/framework/database/query"
	"github.com/gradii/postgres/contracts"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"strings"
	"testing"
)

type configValues struct {
	contractsconfig.Config
	values map[string]any
}

func (c configValues) Get(path string, fallback ...any) any {
	if value, ok := c.values[path]; ok {
		return value
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return nil
}
func (c configValues) GetString(path string, fallback ...string) string {
	if value, ok := c.values[path].(string); ok {
		return value
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}
func (c configValues) GetInt(path string, fallback ...int) int {
	if value, ok := c.values[path].(int); ok {
		return value
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return 0
}
func TestConfigInheritance(t *testing.T) {
	config := NewConfig(configValues{values: map[string]any{
		"database.connections.primary.host": "writer", "database.connections.primary.port": 5433,
		"database.connections.primary.database": "app", "database.connections.primary.username": "user", "database.connections.primary.password": "password",
		"database.connections.primary.schema": "tenant,public", "database.connections.primary.sslmode": "verify-full", "database.connections.primary.timezone": "Asia/Shanghai", "database.connections.primary.prefix": "pre_",
		"database.connections.primary.read": []contracts.Config{{Host: "reader"}, {Dsn: "postgres://reader@localhost/replica", Schema: "replica", Timezone: "UTC", Sslmode: "disable"}},
	}}, "primary")
	writers, readers := config.Writers(), config.Readers()
	require.Len(t, writers, 1)
	require.Len(t, readers, 2)
	expected := contracts.FullConfig{Config: contracts.Config{Host: "writer", Port: 5433, Database: "app", Username: "user", Password: "password", Schema: "tenant,public", Sslmode: "verify-full", Timezone: "Asia/Shanghai"}, Connection: "primary", Driver: Name, Prefix: "pre_"}
	require.Equal(t, expected, writers[0])
	expected.Host = "reader"
	require.Equal(t, expected, readers[0])
	require.Equal(t, "replica", readers[1].Schema)
	require.Equal(t, "UTC", readers[1].Timezone)
	require.Equal(t, "disable", readers[1].Sslmode)
}
func TestConfigDSNAndTimezoneFallback(t *testing.T) {
	for _, timezone := range []string{"", "Europe/Paris"} {
		config := NewConfig(configValues{values: map[string]any{"database.connections.pg.dsn": "native dsn", "app.timezone": timezone, "database.connections.pg.write": []contracts.Config{{}, {Dsn: "override"}}}}, "pg")
		writers := config.Writers()
		require.Len(t, writers, 2)
		require.Equal(t, "native dsn", writers[0].Dsn)
		require.Equal(t, "override", writers[1].Dsn)
		if timezone == "" {
			timezone = "UTC"
		}
		require.Equal(t, timezone, writers[0].Timezone)
		require.Equal(t, "public", writers[0].Schema)
		require.Nil(t, config.Readers())
	}
}
func TestConfigEmptyEndpoints(t *testing.T) {
	config := NewConfig(configValues{values: map[string]any{"database.connections.pg.write": []contracts.Config{}}}, "pg")
	require.Nil(t, config.Writers())
}

// ConfigTestSuite ports the original endpoint/default cases onto the native
// configuration contract. Table names are explicit; Go naming strategies are ignored.
type ConfigTestSuite struct {
	suite.Suite
	config *Config
	values map[string]any
}

func TestConfigTestSuite(t *testing.T) { suite.Run(t, new(ConfigTestSuite)) }

func (s *ConfigTestSuite) SetupTest() {
	s.values = make(map[string]any)
	for key, value := range map[string]any{
		"dsn": "dsn", "host": "localhost", "port": 3306,
		"database": "forge", "username": "root", "password": "123123",
		"prefix": "goravel_", "sslmode": "disable", "timezone": "UTC", "schema": "public",
	} {
		s.values["database.connections.postgres."+key] = value
	}
	s.config = NewConfig(configValues{values: s.values}, "postgres")
}

func (s *ConfigTestSuite) expectedConfig() contracts.FullConfig {
	endpoint := contracts.Config{Dsn: "dsn", Host: "localhost", Port: 3306, Database: "forge", Username: "root", Password: "123123"}
	endpoint.Sslmode = "disable"
	endpoint.Timezone = "UTC"
	endpoint.Schema = "public"
	return contracts.FullConfig{Config: endpoint, Connection: "postgres", Driver: Name, Prefix: "goravel_"}
}

func (s *ConfigTestSuite) TestReads() {
	s.Run("success when configs is empty", func() {
		s.Nil(s.config.Readers())
	})
	s.Run("success when configs is not empty", func() {
		s.values["database.connections.postgres.read"] = []contracts.Config{contracts.Config{Dsn: "dsn", Host: "localhost", Port: 3306, Database: "forge", Username: "root", Password: "123123"}}
		s.Equal([]contracts.FullConfig{s.expectedConfig()}, s.config.Readers())
	})
}

func (s *ConfigTestSuite) TestWrites() {
	for _, test := range []struct {
		name      string
		endpoints any
	}{
		{"success when configs is empty", nil},
		{"success when configs is not empty", []contracts.Config{{Database: "forge"}}},
	} {
		s.Run(test.name, func() {
			s.values["database.connections.postgres.write"] = test.endpoints
			s.Equal([]contracts.FullConfig{s.expectedConfig()}, s.config.Writers())
		})
	}
}

func (s *ConfigTestSuite) TestFillDefault() {
	for _, test := range []struct {
		name        string
		endpoints   []contracts.Config
		appTimezone bool
	}{
		{name: "success when configs is empty", endpoints: []contracts.Config{}},
		{name: "success when configs have item but key is empty", endpoints: []contracts.Config{{}}},
		{name: "success when configs have item", endpoints: []contracts.Config{contracts.Config{Dsn: "dsn", Host: "localhost", Port: 3306, Database: "forge", Username: "root", Password: "123123"}}},
		{name: "success with app.timezone", endpoints: []contracts.Config{contracts.Config{Dsn: "dsn", Host: "localhost", Port: 3306, Database: "forge", Username: "root", Password: "123123"}}, appTimezone: true},
	} {
		s.Run(test.name, func() {
			// These were present in the original default cases. The native
			// configuration keeps endpoint options without inferring table names.
			s.values["database.connections.postgres.singular"] = false
			s.values["database.connections.postgres.no_lower_case"] = true
			s.values["database.connections.postgres.name_replacer"] = strings.NewReplacer("a", "b")
			if test.appTimezone {
				s.values["database.connections.postgres.timezone"] = ""
				s.values["app.timezone"] = "UTC"
			}
			var expected []contracts.FullConfig
			if len(test.endpoints) > 0 {
				expected = []contracts.FullConfig{s.expectedConfig()}
			}
			s.Equal(expected, s.config.fillDefault(test.endpoints))
		})
	}
}

func (s *ConfigTestSuite) TestExplicitTableNames() {
	for _, singular := range []bool{false, true} {
		s.values["database.connections.postgres.singular"] = singular
		s.values["database.connections.postgres.no_lower_case"] = true
		s.values["database.connections.postgres.name_replacer"] = strings.NewReplacer("Data", "Unexpected")
		driver := NewPostgres(configValues{values: s.values}, nil, nil, "postgres")
		endpoint := driver.Pool().Writers[0]
		s.False(endpoint.Singular)
		s.False(endpoint.NoLowerCase)
		s.Nil(endpoint.NameReplacer)
		query := querybuilder.NewBuilder(NewQueryGrammar(endpoint.Prefix)).From("Data", "")
		s.Equal("select * from \"goravel_Data\"", query.ToSQL())
	}
}

func (s *ConfigTestSuite) TestFillDefaultPreservesEndpointValues() {
	endpoint := contracts.Config{Dsn: "endpoint-dsn", Host: "endpoint-host", Port: 5433, Database: "endpoint-database", Username: "endpoint-user", Password: "endpoint-password", Schema: "endpoint_schema", Sslmode: "verify-full", Timezone: "Europe/Paris"}
	expected := contracts.FullConfig{Config: endpoint, Connection: "postgres", Driver: Name, Prefix: "goravel_"}
	s.Equal([]contracts.FullConfig{expected}, s.config.fillDefault([]contracts.Config{endpoint}))
}
