package postgres

import (
	"database/sql/driver"
	"github.com/gradii/framework/contracts/database"
	contractsdriver "github.com/gradii/framework/contracts/database/driver"
	contractsquery "github.com/gradii/framework/contracts/database/query"
)

var _ contractsdriver.Dialect = (*Dialect)(nil)
var _ contractsdriver.DialectWithEscaper = (*Dialect)(nil)
var _ contractsdriver.DialectWithServerVersion = (*Dialect)(nil)

type Dialect struct{}

func NewDialect() *Dialect                { return &Dialect{} }
func (*Dialect) Name() string             { return Name }
func (*Dialect) ServerVersionSQL() string { return "show server_version" }
func (*Dialect) NewConnector(endpoint database.Config) (driver.Connector, error) {
	return NewConnector(endpoint)
}
func (*Dialect) NewQueryGrammar(options contractsdriver.GrammarOptions) contractsquery.Grammar {
	return NewQueryGrammar(options.Prefix)
}
func (*Dialect) NewProcessor() contractsquery.Processor { return NewQueryProcessor() }
