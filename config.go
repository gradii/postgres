package postgres

import (
	"database/sql/driver"
	"github.com/gradii/framework/contracts/config"
	"github.com/gradii/postgres/contracts"
	"strings"
)

type Config struct {
	config     config.Config
	connection string
}

func NewConfig(config config.Config, connection string) *Config {
	return &Config{config: config, connection: connection}
}
func (r *Config) Config() config.Config { return r.config }
func (r *Config) Connection() string    { return r.connection }
func (r *Config) Readers() []contracts.FullConfig {
	values, ok := r.config.Get("database.connections." + r.connection + ".read").([]contracts.Config)
	if !ok {
		return nil
	}
	return r.fillDefault(values)
}
func (r *Config) Writers() []contracts.FullConfig {
	values, ok := r.config.Get("database.connections." + r.connection + ".write").([]contracts.Config)
	if !ok {
		values = []contracts.Config{{}}
	}
	return r.fillDefault(values)
}
func (r *Config) fillDefault(values []contracts.Config) []contracts.FullConfig {
	if len(values) == 0 {
		return nil
	}
	result := make([]contracts.FullConfig, 0, len(values))
	path := "database.connections." + r.connection + "."
	for _, value := range values {
		if value.Connector == nil {
			value.Connector, _ = r.config.Get(path + "connector").(driver.Connector)
		}
		for _, item := range []struct {
			field *string
			name  string
		}{{&value.Dsn, "dsn"}, {&value.Host, "host"}, {&value.Username, "username"}, {&value.Password, "password"}, {&value.Database, "database"}, {&value.Sslmode, "sslmode"}, {&value.Timezone, "timezone"}} {
			if *item.field == "" {
				*item.field = r.config.GetString(path + item.name)
			}
		}
		if value.Port == 0 {
			value.Port = r.config.GetInt(path + "port")
		}
		if value.Schema == "" {
			switch searchPath := r.config.Get(path + "search_path").(type) {
			case string:
				value.Schema = searchPath
			case []string:
				quoted := make([]string, len(searchPath))
				for i, name := range searchPath {
					quoted[i] = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
				}
				value.Schema = strings.Join(quoted, ",")
			}
			if value.Schema == "" {
				value.Schema = r.config.GetString(path+"schema", "public")
			}
		}
		if value.Timezone == "" {
			value.Timezone = r.config.GetString("app.timezone", "UTC")
		}
		if value.Timezone == "" {
			value.Timezone = "UTC"
		}
		result = append(result, contracts.FullConfig{Config: value, Driver: Name, Connection: r.connection, Prefix: r.config.GetString(path + "prefix")})
	}
	return result
}
