package postgres

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/gradii/framework/contracts/database/driver"
	"github.com/gradii/framework/database/schema"
	"github.com/gradii/framework/errors"
	"github.com/spf13/cast"
)

func enumConstraintName(table, column string) string {
	sum := sha256.Sum256([]byte(table + "\x00" + column))
	return fmt.Sprintf("gradii_enum_%x", sum[:12])
}
func (r *SchemaGrammar) enumName(blueprint driver.Blueprint, column string) string {
	_, table, _ := parseSchemaAndTable(blueprint.GetTableName(), "")
	return enumConstraintName(r.prefix+table, column)
}
func (r *SchemaGrammar) enumCheck(column driver.ColumnDefinition) string {
	values := make([]string, len(column.GetAllowed()))
	for i, value := range column.GetAllowed() {
		values[i] = schema.ColumnDefaultValue(cast.ToString(value))
	}
	return r.wrap.Value(column.GetName()) + " in (" + strings.Join(values, ", ") + ")"
}

func (r *SchemaGrammar) CompileSchemaBlueprint(blueprint driver.Blueprint, catalog driver.SchemaCatalog) ([]driver.SchemaStatement, bool, error) {
	if blueprint == nil || r.enumChanges != nil {
		return nil, false, nil
	}
	var commands []*driver.Command
	for _, command := range blueprint.GetCommands() {
		if command.Name == schema.CommandAdd && !command.ShouldBeSkipped && command.Column.IsChange() && command.Column.GetType() == "enum" {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 {
		return nil, false, nil
	}
	if catalog == nil {
		return nil, true, errors.SchemaEnumCatalogRequired.Args(commands[0].Column.GetName())
	}
	compiler, ok := blueprint.(interface {
		CompileStatements(driver.Grammar) ([]driver.SchemaStatement, error)
	})
	if !ok {
		return nil, true, errors.SchemaFeatureNotSupported.Args(Name, "enum planning for this blueprint")
	}
	namespace, table, err := catalog.ResolveTable(blueprint.GetTableName())
	if err != nil {
		return nil, true, err
	}
	// Replacements belong to this compilation; the shared grammar and the
	// ordering of unrelated blueprint commands remain untouched.
	planned := *r
	planned.enumChanges = make(map[*driver.Command][]driver.SchemaStatement)
	for _, command := range commands {
		column := command.Column
		rows, err := catalog.Select(driver.SchemaStatement{
			SQL:      "select c.conname as name, pg_get_expr(c.conbin, c.conrelid) as definition, cardinality(c.conkey) = 1 as single_column from pg_constraint c join pg_class t on t.oid = c.conrelid join pg_namespace n on n.oid = t.relnamespace join pg_attribute a on a.attrelid = t.oid and a.attnum = any(c.conkey) where c.contype = 'c' and t.relname = ? and n.nspname = coalesce(nullif(?, ''), current_schema()) and a.attname = ?",
			Bindings: []any{table, namespace, column.GetName()},
		})
		if err != nil {
			return nil, true, err
		}
		constraint, err := enumConstraint(rows, table, column.GetName())
		if err != nil {
			return nil, true, err
		}
		var statements []driver.SchemaStatement
		if constraint != "" {
			statements = append(statements, driver.SchemaStatement{SQL: "alter table " + r.wrap.Table(blueprint.GetTableName()) + " drop constraint " + r.wrap.Value(constraint)})
		}
		changed, _, err := planned.CompileSchemaCommand(blueprint, command)
		if err != nil {
			return nil, true, err
		}
		statements = append(statements, changed...)
		statements = append(statements, driver.SchemaStatement{SQL: "alter table " + r.wrap.Table(blueprint.GetTableName()) + " add constraint " + r.wrap.Value(r.enumName(blueprint, column.GetName())) + " check (" + r.enumCheck(column) + ")"})
		planned.enumChanges[command] = statements
	}
	statements, err := compiler.CompileStatements(&planned)
	return statements, true, err
}

// Only framework names and PostgreSQL's legacy inline-check names establish
// ownership. A custom membership check is not ours to remove.
func enumConstraint(rows []map[string]any, table, column string) (string, error) {
	found := ""
	for _, row := range rows {
		name := cast.ToString(row["name"])
		legacy := table + "_" + column + "_check"
		marked := isEnumConstraintName(name)
		owned := marked || name == legacy
		if strings.HasPrefix(name, legacy) && len(name) > len(legacy) {
			owned = true
			for _, c := range name[len(legacy):] {
				if c < '0' || c > '9' {
					owned = false
					break
				}
			}
		}
		membership := enumMembership(cast.ToString(row["definition"]), column)
		if marked && !membership {
			return "", errors.SchemaEnumConstraintAmbiguous.Args(column, table)
		}
		if owned && membership {
			if !cast.ToBool(row["single_column"]) || found != "" {
				return "", errors.SchemaEnumConstraintAmbiguous.Args(column, table)
			}
			found = name
		} else if membership {
			return "", errors.SchemaEnumConstraintAmbiguous.Args(column, table)
		}
	}
	if found == "" && len(rows) != 0 {
		return "", errors.SchemaEnumConstraintAmbiguous.Args(column, table)
	}
	return found, nil
}
func isEnumConstraintName(name string) bool {
	if len(name) != len("gradii_enum_")+24 || !strings.HasPrefix(name, "gradii_enum_") {
		return false
	}
	for _, c := range strings.TrimPrefix(name, "gradii_enum_") {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
