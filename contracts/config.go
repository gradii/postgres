package contracts

import (
	"database/sql/driver"
	contractsconfig "github.com/gradii/framework/contracts/config"
)

type ConfigBuilder interface {
	Config() contractsconfig.Config
	Connection() string
	Readers() []FullConfig
	Writers() []FullConfig
}

// Deprecated: declare stable table and column names explicitly.
type Replacer interface{ Replace(string) string }
type Config struct {
	Connector                                                          driver.Connector
	Dsn, Host, Database, Username, Password, Schema, Sslmode, Timezone string
	Port                                                               int
}
type FullConfig struct {
	Config
	Driver, Connection, Prefix string
}
