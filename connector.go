package postgres

import (
	"database/sql/driver"
	"github.com/gradii/framework/contracts/database"
	"github.com/gradii/framework/errors"
	postgreserrors "github.com/gradii/postgres/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// NewConnector validates settings without opening a pool or contacting PostgreSQL.
func NewConnector(endpoint database.Config) (driver.Connector, error) {
	if endpoint.Connector != nil {
		return endpoint.Connector, nil
	}
	config, err := connectorConfig(endpoint)
	if err != nil {
		return nil, err
	}
	return stdlib.GetConnector(*config), nil
}
func connectorConfig(endpoint database.Config) (*pgx.ConnConfig, error) {
	dsn := endpoint.Dsn
	if dsn == "" {
		if endpoint.Host == "" {
			return nil, errors.DatabaseConfigNotFound
		}
		port := endpoint.Port
		if port == 0 {
			port = 5432
		}
		if port < 1 || port > 65535 {
			return nil, postgreserrors.InvalidPort.Args(port)
		}
		address := url.URL{Scheme: "postgres", User: url.UserPassword(endpoint.Username, endpoint.Password), Host: net.JoinHostPort(strings.Trim(endpoint.Host, "[]"), strconv.Itoa(port)), Path: "/" + endpoint.Database}
		options := url.Values{}
		if endpoint.Sslmode != "" {
			options.Set("sslmode", endpoint.Sslmode)
		}
		if endpoint.Timezone != "" {
			options.Set("timezone", endpoint.Timezone)
		}
		if endpoint.Schema != "" {
			options.Set("search_path", endpoint.Schema)
		}
		address.RawQuery = options.Encode()
		dsn = address.String()
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// Keep migration support: changing a row shape must not reuse a cached plan.
	parsed.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return parsed, nil
}
