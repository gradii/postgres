# PostgreSQL

PostgreSQL runtime driver for `github.com/gradii/framework`, forked from
[goravel/postgres](https://github.com/goravel/postgres). The runtime uses
`database/sql` and `pgx`, with no GORM dependency.

Register `&postgres.ServiceProvider{}` and configure a connection's `via` callback
to return `facades.Postgres("postgres")` from `github.com/gradii/postgres/facades`.
The callback retains the framework's `func() (driver.Driver, error)` contract.
The framework owns pools, transactions and routing; this module supplies connectors,
query and schema grammars, result processors and PostgreSQL literal rendering.

Connection fields include `host`, `port`, `database`, `username`, `password`,
`schema` (the PostgreSQL search path), `sslmode`, `timezone`, `prefix`, and `dsn`.
Read/write entries use `[]contracts.Config` and inherit empty fields from the named
connection. An explicit connector wins over a DSN; an explicit native DSN preserves
its TLS, search-path, timezone and other connection options. Both generated and
explicit DSNs use pgx simple protocol to preserve migration compatibility when
result shapes change. `Pool()` does not perform connector validation or network I/O.

Timezone falls back to `app.timezone`, then UTC. Schema defaults to `public` for
generated connections. DSN-only schema introspection uses the actual server search
path. `prefix` remains supported; `singular`, `name_replacer`, and `no_lower_case`
are removed. Model table and column names must be explicit.

Stored generated columns require PostgreSQL 12; virtual generated columns require
PostgreSQL 18. Older metadata queries omit catalog fields absent from the server.
PostGIS geometry/geography and pgvector SQL compilation preserve their extension
types; running those statements requires the corresponding server extension.

## Development and tests

Release requires a matching published framework revision containing the new driver
contracts. For sibling checkouts, use an ignored local workspace:

```sh
go work init . ../goravel-framework ../mysql
go work edit -replace=github.com/gradii/framework@v1.18.0=../goravel-framework
go test -race ./...
go tool mockery
```

Set `GRADII_TEST_POSTGRES_DSN` (or `GORAVEL_TEST_POSTGRES_DSN`) to an existing test
database to run `go test -run TestFrameworkIntegration -v .`. The test creates and
removes a unique schema and verifies DB/ORM CRUD, JSONB, explicit mappings, RETURNING
IDs, single-connection transactions, schema metadata and transactional DDL.
Live integration was verified against PostgreSQL 18.3. Earlier generated-column
boundaries and PostGIS/pgvector types have compilation tests, without live extension
or historical-server verification.

Upstream test cases have been ported to the new query/schema/connector APIs.
Database-fixture lifecycle coverage lives in the framework integration module.
No old implementation or GORM compatibility module is retained. Docker and
automatic installation tooling are outside the runtime module; the compatibility
`Driver.Docker()` method returns an unsupported error.
