package postgres

import (
	"fmt"
	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"strconv"
	"strings"
)

var _ driver.SchemaCompiler = (*SchemaGrammar)(nil)
var _ driver.SchemaBlueprintPlanner = (*SchemaGrammar)(nil)

func describeColumn(column driver.ColumnDefinition) driver.ColumnDescription {
	if readable, ok := column.(driver.ColumnDescriber); ok {
		return readable.DescribeColumn()
	}
	return driver.ColumnDescription{}
}
func describeBlueprint(blueprint driver.Blueprint) driver.BlueprintDescription {
	if readable, ok := blueprint.(driver.BlueprintDescriber); ok {
		return readable.DescribeBlueprint()
	}
	return driver.BlueprintDescription{Table: blueprint.GetTableName(), Columns: blueprint.GetAddedColumns(), Commands: blueprint.GetCommands()}
}
func schemaExpression(value any) (string, error) {
	switch value := value.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case schema.Expression:
		return string(value), nil
	case interface{ SQL() string }:
		return value.SQL(), nil
	case interface{ GetValue() string }:
		return value.GetValue(), nil
	default:
		return "", errors.DatabaseUnsupportedType.Args(fmt.Sprintf("%T", value), "SQL string or expression")
	}
}
func (r *SchemaGrammar) schemaColumnType(column driver.ColumnDefinition) (string, error) {
	options := describeColumn(column)
	switch column.GetType() {
	case "raw":
		return options.RawDefinition, nil
	case "binary":
		return "bytea", nil
	case "year":
		return "integer", nil
	case "ipAddress":
		return "inet", nil
	case "macAddress":
		return "macaddr", nil
	case "set":
		return "", errors.SchemaFeatureNotSupported.Args(Name, "set columns")
	case "vector":
		if options.Dimensions != nil {
			return fmt.Sprintf("vector(%d)", *options.Dimensions), nil
		}
		return "vector", nil
	case "computed":
		return "", errors.SchemaComputedRequiresType
	case "geometry", "geography":
		kind := column.GetType()
		if options.Subtype != "" {
			kind += "(" + strings.ToLower(options.Subtype)
			if options.Srid != nil && *options.Srid != 0 {
				kind += "," + strconv.Itoa(*options.Srid)
			}
			kind += ")"
		}
		return kind, nil
	default:
		return schema.ColumnType(r, column), nil
	}
}
func (r *SchemaGrammar) schemaColumn(blueprint driver.Blueprint, column driver.ColumnDefinition) (string, error) {
	kind, err := r.schemaColumnType(column)
	if err != nil {
		return "", err
	}
	options := describeColumn(column)
	sql := r.wrap.Column(column.GetName()) + " " + kind
	if options.Collation != "" {
		sql += " collate " + r.wrap.Value(options.Collation)
	}
	sql += r.ModifyNullable(blueprint, column) + r.ModifyDefault(blueprint, column)
	for _, generated := range []struct {
		value  any
		stored bool
	}{{options.VirtualAs, false}, {options.StoredAs, true}} {
		if generated.value == nil {
			continue
		}
		expression, err := schemaExpression(generated.value)
		if err != nil {
			return "", err
		}
		version, err := r.serverVersion()
		if err != nil {
			return "", err
		}
		major := serverMajor(version)
		if version != "" && ((generated.stored && major < 12) || (!generated.stored && major < 18)) {
			return "", errors.SchemaFeatureNotSupported.Args(Name, "generated column kind on PostgreSQL "+version)
		}
		sql += " generated always as (" + expression + ")"
		if generated.stored {
			sql += " stored"
		} else {
			sql += " virtual"
		}
	}
	sql += r.ModifyGeneratedAs(blueprint, column) + r.ModifyIncrement(blueprint, column)
	return sql, nil
}
func (r *SchemaGrammar) CompileSchemaCommand(blueprint driver.Blueprint, command *driver.Command) ([]driver.SchemaStatement, bool, error) {
	table := r.wrap.Table(blueprint.GetTableName())
	var sql string
	switch command.Name {
	case schema.CommandCreate:
		options := describeBlueprint(blueprint)
		columns := make([]string, 0, len(options.Columns))
		for _, column := range options.Columns {
			definition, err := r.schemaColumn(blueprint, column)
			if err != nil {
				return nil, true, err
			}
			columns = append(columns, definition)
		}
		sql = "create "
		if options.Temporary {
			sql += "temporary "
		}
		sql += "table " + table + " (" + strings.Join(columns, ", ") + ")"
	case schema.CommandAdd:
		column := command.Column
		options := describeColumn(column)
		if column.IsChange() {
			if (options.StoredSet && options.StoredAs != nil) || (options.VirtualSet && options.VirtualAs != nil) {
				return nil, true, errors.SchemaGeneratedColumnChange
			}
			kind, err := r.schemaColumnType(column)
			if err != nil {
				return nil, true, err
			}
			if options.Collation != "" {
				kind += " collate " + r.wrap.Value(options.Collation)
			}
			changes := []string{"type " + kind}
			for _, modifier := range []func(driver.Blueprint, driver.ColumnDefinition) string{r.ModifyNullable, r.ModifyDefault, r.ModifyGeneratedAsForChange, r.ModifyGeneratedAs} {
				if value := strings.TrimSpace(modifier(blueprint, column)); value != "" {
					changes = append(changes, value)
				}
			}
			if options.StoredSet || options.VirtualSet {
				changes = append(changes, "drop expression if exists")
			}
			for i := range changes {
				changes[i] = "alter column " + r.wrap.Value(column.GetName()) + " " + changes[i]
			}
			sql = "alter table " + table + " " + strings.Join(changes, ", ")
		} else {
			definition, err := r.schemaColumn(blueprint, column)
			if err != nil {
				return nil, true, err
			}
			sql = "alter table " + table + " add column " + definition
		}
	case "spatialIndex":
		sql = "create index " + r.wrap.Value(command.Index) + " on " + table + " using gist (" + r.wrap.Columnize(command.Columns) + ")"
	case "dropSpatialIndex":
		sql = "drop index " + r.wrap.Value(command.Index)
	case "rawIndex":
		sql = "create index " + r.wrap.Value(command.Index) + " on " + table + " (" + command.Value + ")"
	default:
		return nil, false, nil
	}
	return []driver.SchemaStatement{{SQL: sql}}, true, nil
}
func (r *SchemaGrammar) CompileSchemaStartingValues(blueprint driver.Blueprint) ([]driver.SchemaStatement, error) {
	var result []driver.SchemaStatement
	namespace, table, err := parseSchemaAndTable(blueprint.GetTableName(), "")
	if err != nil {
		return nil, err
	}
	for _, column := range blueprint.GetAddedColumns() {
		value := describeColumn(column).StartingValue
		if value == nil || !column.GetAutoIncrement() || *value == 0 {
			continue
		}
		sequence := r.prefix + table + "_" + column.GetName() + "_seq"
		if namespace != "" {
			sequence = namespace + "." + sequence
		}
		result = append(result, driver.SchemaStatement{SQL: "alter sequence " + schema.NewWrap("").Table(sequence) + " restart with " + strconv.FormatUint(*value, 10)})
	}
	return result, nil
}
func (r *SchemaGrammar) SchemaExecutionPolicy(driver.Blueprint) driver.SchemaExecutionPolicy {
	return driver.SchemaExecutionPolicy{Transactional: true}
}
func (r *SchemaGrammar) CompileSchemaBlueprint(driver.Blueprint, driver.SchemaCatalog) ([]driver.SchemaStatement, bool, error) {
	return nil, false, nil
}
func serverMajor(version string) int {
	var major int
	_, _ = fmt.Sscanf(strings.TrimPrefix(version, "PostgreSQL "), "%d", &major)
	return major
}
