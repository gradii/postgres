package postgres

import (
	contractsquery "github.com/gradii/framework/contracts/database/query"
	"github.com/gradii/framework/database/query/processors"
	"strings"
)

// QueryProcessor reads the row produced by RETURNING on the write connection.
// This ports QueryProcessor::processInsertGetId without inferring the result shape from SQL text.
type QueryProcessor struct{ processors.BaseProcessor }

func (p *QueryProcessor) ProcessInsertGetID(connection contractsquery.Connection, statement string, values []any, sequence string) (any, error) {
	if sequence == "" {
		sequence = "id"
	}

	return connection.SelectFromWriteConnection(statement, values, sequence)
}

// ProcessColumns ports QueryProcessor::processColumns.
func (p *QueryProcessor) ProcessColumns(results []contractsquery.Row, sql ...string) []contractsquery.Row {
	columns := make([]contractsquery.Row, 0, len(results))
	for _, result := range results {
		defaultValue := result["default"]
		var generation any
		if processors.MetadataBool(result["generated"]) {
			var kind any
			switch result["generated"] {
			case "s":
				kind = "stored"
			case "v":
				kind = "virtual"
			}
			generation = contractsquery.Row{"type": kind, "expression": defaultValue}
			defaultValue = nil
		}
		columns = append(columns, contractsquery.Row{
			"name": result["name"], "type_name": result["type_name"], "type": result["type"],
			"collation": result["collation"], "nullable": processors.MetadataBool(result["nullable"]),
			"default": defaultValue, "auto_increment": strings.HasPrefix(processors.MetadataString(result["default"]), "nextval(") || processors.MetadataString(result["identity"]) != "",
			"comment": result["comment"], "generation": generation,
		})
	}
	return columns
}

func NewQueryProcessor() *QueryProcessor { return &QueryProcessor{} }
func (p *QueryProcessor) ProcessTypes(results []contractsquery.Row) []contractsquery.Row {
	types := map[string]any{"b": "base", "c": "composite", "d": "domain", "e": "enum", "p": "pseudo", "r": "range", "m": "multirange"}
	categories := map[string]any{"a": "array", "b": "boolean", "c": "composite", "d": "date_time", "e": "enum", "g": "geometric",
		"i": "network_address", "n": "numeric", "p": "pseudo", "r": "range", "s": "string", "t": "timespan", "u": "user_defined",
		"v": "bit_string", "x": "unknown", "z": "internal_use"}
	result := make([]contractsquery.Row, 0, len(results))
	for _, row := range results {
		result = append(result, contractsquery.Row{"name": row["name"], "schema": row["schema"], "schema_qualified_name": qualifiedName(row),
			"implicit": processors.MetadataBool(row["implicit"]), "type": types[strings.ToLower(processors.MetadataString(row["type"]))],
			"category": categories[strings.ToLower(processors.MetadataString(row["category"]))]})
	}
	return result
}

func qualifiedName(row contractsquery.Row) string {
	if row["schema"] != nil {
		return processors.MetadataString(row["schema"]) + "." + processors.MetadataString(row["name"])
	}
	return processors.MetadataString(row["name"])
}
func (p *QueryProcessor) ProcessIndexes(rows []contractsquery.Row) []contractsquery.Row {
	result := make([]contractsquery.Row, 0, len(rows))
	for _, row := range rows {
		result = append(result, contractsquery.Row{"name": strings.ToLower(processors.MetadataString(row["name"])), "columns": processors.MetadataColumns(row["columns"]), "type": strings.ToLower(processors.MetadataString(row["type"])), "unique": processors.MetadataBool(row["unique"]), "primary": processors.MetadataBool(row["primary"])})
	}
	return result
}
func (p *QueryProcessor) ProcessForeignKeys(rows []contractsquery.Row) []contractsquery.Row {
	action := func(value any) string {
		valueString := strings.ToLower(processors.MetadataString(value))
		if expanded, ok := map[string]string{"a": "no action", "r": "restrict", "c": "cascade", "n": "set null", "d": "set default"}[valueString]; ok {
			return expanded
		}
		return valueString
	}
	result := make([]contractsquery.Row, 0, len(rows))
	for _, row := range rows {
		result = append(result, contractsquery.Row{"name": row["name"], "columns": processors.MetadataColumns(row["columns"]), "foreign_schema": row["foreign_schema"], "foreign_table": row["foreign_table"], "foreign_columns": processors.MetadataColumns(row["foreign_columns"]), "on_update": action(row["on_update"]), "on_delete": action(row["on_delete"])})
	}
	return result
}
