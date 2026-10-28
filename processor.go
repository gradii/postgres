package postgres

import (
	"github.com/gradii/framework/contracts/database/driver"
	contractsquery "github.com/gradii/framework/contracts/database/query"
	"github.com/spf13/cast"
)

// Processor adapts the legacy typed metadata contract to the canonical query processor.
type Processor struct{ query contractsquery.Processor }

var _ driver.Processor = (*Processor)(nil)

func NewProcessor() *Processor { return &Processor{query: NewQueryProcessor()} }
func (r Processor) processor() contractsquery.Processor {
	if r.query != nil {
		return r.query
	}
	return NewQueryProcessor()
}
func (r Processor) ProcessColumns(values []driver.DBColumn) []driver.Column {
	rows := make([]contractsquery.Row, len(values))
	for i, value := range values {
		rows[i] = contractsquery.Row{"name": value.Name, "type_name": value.TypeName, "type": value.Type, "collation": value.Collation, "nullable": cast.ToBool(value.Nullable), "default": value.Default, "comment": value.Comment, "extra": value.Extra}
	}
	var columns []driver.Column
	for _, row := range r.processor().ProcessColumns(rows) {
		columns = append(columns, driver.Column{Name: cast.ToString(row["name"]), TypeName: cast.ToString(row["type_name"]), Type: cast.ToString(row["type"]), Collation: cast.ToString(row["collation"]), Nullable: cast.ToBool(row["nullable"]), Default: cast.ToString(row["default"]), Comment: cast.ToString(row["comment"]), Autoincrement: cast.ToBool(row["auto_increment"])})
	}
	return columns
}
func (r Processor) ProcessIndexes(values []driver.DBIndex) []driver.Index {
	rows := make([]contractsquery.Row, len(values))
	for i, value := range values {
		rows[i] = contractsquery.Row{"name": value.Name, "columns": value.Columns, "type": value.Type, "unique": value.Unique, "primary": value.Primary}
	}
	var indexes []driver.Index
	for _, row := range r.processor().ProcessIndexes(rows) {
		indexes = append(indexes, driver.Index{Name: cast.ToString(row["name"]), Columns: cast.ToStringSlice(row["columns"]), Type: cast.ToString(row["type"]), Unique: cast.ToBool(row["unique"]), Primary: cast.ToBool(row["primary"])})
	}
	return indexes
}
func (r Processor) ProcessForeignKeys(values []driver.DBForeignKey) []driver.ForeignKey {
	rows := make([]contractsquery.Row, len(values))
	for i, value := range values {
		rows[i] = contractsquery.Row{"name": value.Name, "columns": value.Columns, "foreign_schema": value.ForeignSchema, "foreign_table": value.ForeignTable, "foreign_columns": value.ForeignColumns, "on_update": value.OnUpdate, "on_delete": value.OnDelete}
	}
	var keys []driver.ForeignKey
	for _, row := range r.processor().ProcessForeignKeys(rows) {
		keys = append(keys, driver.ForeignKey{Name: cast.ToString(row["name"]), Columns: cast.ToStringSlice(row["columns"]), ForeignSchema: cast.ToString(row["foreign_schema"]), ForeignTable: cast.ToString(row["foreign_table"]), ForeignColumns: cast.ToStringSlice(row["foreign_columns"]), OnUpdate: cast.ToString(row["on_update"]), OnDelete: cast.ToString(row["on_delete"])})
	}
	return keys
}
func (r Processor) ProcessTypes(values []driver.Type) []driver.Type {
	rows := make([]contractsquery.Row, len(values))
	for i, value := range values {
		rows[i] = contractsquery.Row{"name": value.Name, "schema": value.Schema, "type": value.Type, "category": value.Category, "implicit": value.Implicit}
	}
	for i, row := range r.processor().ProcessTypes(rows) {
		values[i].Type = cast.ToString(row["type"])
		values[i].Category = cast.ToString(row["category"])
	}
	return values
}
