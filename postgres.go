package postgres

import (
	"github.com/gradii/framework/contracts/config"
	"github.com/gradii/framework/contracts/database"
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/contracts/log"
	"github.com/gradii/framework/contracts/process"
	"github.com/gradii/framework/contracts/testing/docker"
	"github.com/gradii/postgres/contracts"
	postgreserrors "github.com/gradii/postgres/errors"
	"sync"
)

var _ driver.Driver = (*Postgres)(nil)
var _ driver.DriverWithDialect = (*Postgres)(nil)
var _ driver.DriverWithServerVersion = (*Postgres)(nil)

type Postgres struct {
	config  contracts.ConfigBuilder
	mu      sync.RWMutex
	version func() (string, error)
}

// NewPostgres keeps the legacy constructor while the framework owns SQL pools.
func NewPostgres(config config.Config, _ log.Log, _ process.Process, connection string) *Postgres {
	return &Postgres{config: NewConfig(config, connection)}
}
func (r *Postgres) Docker() (docker.DatabaseDriver, error) {
	return nil, postgreserrors.DockerNotSupported
}
func (r *Postgres) Dialect() driver.Dialect { return NewDialect() }
func (r *Postgres) SetServerVersion(resolver func() (string, error)) {
	r.mu.Lock()
	r.version = resolver
	r.mu.Unlock()
}
func (r *Postgres) Grammar() driver.Grammar {
	prefix := ""
	if writers := r.config.Writers(); len(writers) > 0 {
		prefix = writers[0].Prefix
	}
	g := NewSchemaGrammar(prefix)
	r.mu.RLock()
	resolver := r.version
	r.mu.RUnlock()
	g.SetVersionResolver(resolver)
	return g
}
func (r *Postgres) Processor() driver.Processor { return NewProcessor() }
func (r *Postgres) Pool() database.Pool {
	return database.Pool{Readers: r.fullConfigsToConfigs(r.config.Readers()), Writers: r.fullConfigsToConfigs(r.config.Writers())}
}
func (r *Postgres) fullConfigsToConfigs(values []contracts.FullConfig) []database.Config {
	result := make([]database.Config, len(values))
	for i, c := range values {
		if c.Dsn != "" || c.Connector != nil {
			c.Schema = ""
		}
		result[i] = database.Config{Connector: c.Connector, Connection: c.Connection, Driver: Name, Dsn: c.Dsn, Database: c.Database, Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, Prefix: c.Prefix, Schema: c.Schema, Sslmode: c.Sslmode, Timezone: c.Timezone}
	}
	return result
}
