package postgres

import (
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"strings"
)

var _ driver.SchemaOperationsGrammar = (*SchemaGrammar)(nil)
var _ driver.SchemaOperationPlanner = (*SchemaGrammar)(nil)

func (*SchemaGrammar) ForeignKeyStateQuery() string { return "" }
func (*SchemaGrammar) SchemaOperationPolicy(operation driver.SchemaOperation) driver.SchemaExecutionPolicy {
	return driver.SchemaExecutionPolicy{Transactional: strings.HasPrefix(operation.Kind, "dropAll")}
}
func (r *SchemaGrammar) CompileSchemaOperation(operation driver.SchemaOperation) ([]driver.SchemaStatement, error) {
	var sql string
	wrap := schema.NewWrap("")
	switch operation.Kind {
	case "foreignKeys":
		sql = "SET CONSTRAINTS ALL DEFERRED"
		if operation.Enabled {
			sql = "SET CONSTRAINTS ALL IMMEDIATE"
		}
	case "createDatabase":
		sql = "create database " + wrap.Value(operation.Name)
		if operation.Charset != "" {
			sql += " encoding " + schema.ColumnDefaultValue(operation.Charset)
		}
	case "dropDatabase":
		sql = "drop database if exists " + wrap.Value(operation.Name)
	case "dropAllTables":
		var names []string
		for _, table := range operation.Tables {
			if table.Name == "spatial_ref_sys" {
				continue
			}
			name := table.Name
			if table.Schema != "" {
				name = table.Schema + "." + name
			}
			names = append(names, wrap.Table(name))
		}
		if len(names) > 0 {
			sql = "drop table " + strings.Join(names, ", ") + " cascade"
		}
	case "dropAllViews":
		var names []string
		for _, view := range operation.Views {
			name := view.Name
			if view.Schema != "" {
				name = view.Schema + "." + name
			}
			names = append(names, wrap.Table(name))
		}
		if len(names) > 0 {
			sql = "drop view " + strings.Join(names, ", ") + " cascade"
		}
	case "dropAllTypes":
		var types, domains []string
		for _, kind := range operation.Types {
			if kind.Implicit {
				continue
			}
			name := kind.Name
			if kind.Schema != "" {
				name = kind.Schema + "." + name
			}
			if kind.Type == "domain" {
				domains = append(domains, wrap.Table(name))
			} else {
				types = append(types, wrap.Table(name))
			}
		}
		var result []driver.SchemaStatement
		if len(types) > 0 {
			result = append(result, driver.SchemaStatement{SQL: "drop type " + strings.Join(types, ", ") + " cascade"})
		}
		if len(domains) > 0 {
			result = append(result, driver.SchemaStatement{SQL: "drop domain " + strings.Join(domains, ", ") + " cascade"})
		}
		return result, nil
	default:
		return nil, errors.SchemaFeatureNotSupported.Args(Name, operation.Kind)
	}
	if sql == "" {
		return nil, nil
	}
	return []driver.SchemaStatement{{SQL: sql}}, nil
}
