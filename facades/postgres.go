package facades

import (
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/postgres"
	postgreserrors "github.com/gradii/postgres/errors"
)

func Postgres(connection string) (driver.Driver, error) {
	if postgres.App == nil {
		return nil, postgreserrors.ServiceProviderNotRegistered
	}
	instance, err := postgres.App.MakeWith(postgres.Binding, map[string]any{"connection": connection})
	if err != nil {
		return nil, err
	}
	return instance.(*postgres.Postgres), nil
}
