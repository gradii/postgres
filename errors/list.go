package errors

import frameworkerrors "github.com/gradii/framework/errors"

var (
	DockerNotSupported           = frameworkerrors.New("Docker helpers are not supported by the PostgreSQL runtime driver")
	ServiceProviderNotRegistered = frameworkerrors.New("please register the PostgreSQL service provider")
	ConnectionNotSet             = frameworkerrors.New("PostgreSQL connection name is required")
	InvalidPort                  = frameworkerrors.New("invalid PostgreSQL port: %d")
	SchemaColumnNotFound         = frameworkerrors.New("Column %s does not exist")
)
