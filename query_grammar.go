package postgres

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gradii/framework/database/query/core"
	"github.com/gradii/framework/database/query/grammars"
)

var (
	jsonPathArrayKeys = regexp.MustCompile(`(\[[^\]]+\])+$`)
	jsonPathKey       = regexp.MustCompile(`\[([^\]]+)\]`)
)

// QueryGrammar mirrors Illuminate\Database\Query\Grammars\PostgresGrammar. Only the methods
// whose SQL differs from the base are declared here.
type QueryGrammar struct {
	*grammars.BaseGrammar
}

// NewQueryGrammar creates the Postgres grammar.
func NewQueryGrammar(prefix string) *QueryGrammar {
	g := &QueryGrammar{BaseGrammar: grammars.NewBaseGrammar(prefix)}
	g.Attach(g)

	return g
}

// postgresOperators mirrors PostgresGrammar::$operators: the clause operators this dialect accepts.
// It carries the ?-family and the Postgres-specific comparisons that the base Builder list lacks.
var postgresOperators = []string{
	"=", "<", ">", "<=", ">=", "<>", "!=",
	"like", "not like", "between", "ilike", "not ilike",
	"~", "&", "|", "#", "<<", ">>", "<<=", ">>=",
	"&&", "@>", "<@", "?", "?|", "?&", "||", "-", "@?", "@@", "#-",
	"is distinct from", "is not distinct from",
}

var postgresCustomOperators struct {
	sync.RWMutex
	values []string
}

// CustomOperators ports QueryGrammar::customOperators: valid strings are appended process-wide.
func CustomOperators(operators []any) {
	postgresCustomOperators.Lock()
	defer postgresCustomOperators.Unlock()
	for _, value := range operators {
		if operator, ok := value.(string); ok && operator != "" && operator != "0" {
			postgresCustomOperators.values = append(postgresCustomOperators.values, operator)
		}
	}
}

// GetOperators merges the built-in and custom operators into a unique, ordered list.
func (g *QueryGrammar) GetOperators() []string {
	postgresCustomOperators.RLock()
	defer postgresCustomOperators.RUnlock()
	operators := append(append([]string{}, postgresOperators...), postgresCustomOperators.values...)
	seen := make(map[string]bool, len(operators))
	result := make([]string, 0, len(operators))
	for _, operator := range operators {
		if !seen[operator] {
			result = append(result, operator)
			seen[operator] = true
		}
	}
	return result
}

// GetBitwiseOperators mirrors PostgresGrammar::$bitwiseOperators, which adds "#" and the shift
// spellings to the base Builder set so those clauses compile through whereBitwise().
func (g *QueryGrammar) GetBitwiseOperators() []string {
	return []string{"~", "&", "|", "#", "<<", ">>", "<<=", ">>="}
}

// WhereBasic mirrors PostgresGrammar::whereBasic(): a like comparison casts the column to text.
func (g *QueryGrammar) WhereBasic(b core.Query, where core.Where) string {
	if strings.Contains(strings.ToLower(where.Operator), "like") {
		return g.Wrap(where.Column) + "::text " + where.Operator + " " + g.Parameter(where.Value)
	}

	return g.BaseGrammar.WhereBasic(b, where)
}

// WhereDate mirrors PostgresGrammar::whereDate().
func (g *QueryGrammar) WhereDate(b core.Query, where core.Where) string {
	column := g.Wrap(where.Column)
	if g.IsJsonSelector(core.AsString(where.Column)) {
		column = "(" + column + ")"
	}

	return column + "::date " + where.Operator + " " + g.Parameter(where.Value)
}

// dateBasedWhere mirrors PostgresGrammar::dateBasedWhere().
func (g *QueryGrammar) dateBasedWhere(typ string, where core.Where) string {
	return "extract(" + typ + " from " + g.Wrap(where.Column) + ") " + where.Operator + " " + g.Parameter(where.Value)
}

// WhereTime mirrors PostgresGrammar::whereTime(): the column is cast to time rather than read out
// with extract(), and a JSON selector is parenthesised before the cast the way the date clause is.
func (g *QueryGrammar) WhereTime(b core.Query, where core.Where) string {
	column := g.Wrap(where.Column)
	if g.IsJsonSelector(core.AsString(where.Column)) {
		column = "(" + column + ")"
	}

	return column + "::time " + where.Operator + " " + g.Parameter(where.Value)
}

// WhereDay mirrors PostgresGrammar::whereDay().
func (g *QueryGrammar) WhereDay(b core.Query, where core.Where) string {
	return g.dateBasedWhere("day", where)
}

// WhereMonth mirrors PostgresGrammar::whereMonth().
func (g *QueryGrammar) WhereMonth(b core.Query, where core.Where) string {
	return g.dateBasedWhere("month", where)
}

// WhereYear mirrors PostgresGrammar::whereYear().
func (g *QueryGrammar) WhereYear(b core.Query, where core.Where) string {
	return g.dateBasedWhere("year", where)
}

// CompileColumns mirrors PostgresGrammar::compileColumns(), which supports distinct on.
func (g *QueryGrammar) CompileColumns(b core.Query, columns []any) string {
	if b.State().Aggregate != nil {
		return ""
	}

	switch {
	case len(b.State().DistinctColumns) > 0:
		return "select distinct on (" + g.Columnize(b.State().DistinctColumns) + ") " + g.Columnize(columns)
	case b.State().Distinct:
		return "select distinct " + g.Columnize(columns)
	default:
		return "select " + g.Columnize(columns)
	}
}

// CompileInsertGetId mirrors PostgresGrammar::compileInsertGetId(): insert then returning.
func (g *QueryGrammar) CompileInsertGetId(b core.Query, values []map[string]any, sequence string) string {
	if sequence == "" {
		sequence = "id"
	}

	return g.CompileInsert(b, values) + " returning " + g.Wrap(sequence)
}

// CompileUpsert mirrors PostgresGrammar::compileUpsert(): on conflict do update.
func (g *QueryGrammar) CompileUpsert(b core.Query, values []map[string]any, uniqueBy []string, update []core.UpsertUpdate) string {
	sql := g.CompileInsert(b, values)
	sql += " on conflict (" + g.Columnize(core.StringsToAny(uniqueBy)) + ") do update set "

	parts := make([]string, 0, len(update))
	for _, item := range update {
		if item.Bound {
			parts = append(parts, g.Wrap(item.Column)+" = "+g.Parameter(item.Value))
			continue
		}
		parts = append(parts, g.Wrap(item.Column)+" = "+g.WrapValue("excluded")+"."+g.Wrap(item.Column))
	}

	return sql + strings.Join(parts, ", ")
}

// postgresTruncateWithoutCascade mirrors PostgresGrammar::$cascadeTruncate, inverted. Laravel's flag
// is a static property, so it is a package-level toggle here rather than a grammar field: one value
// governs every QueryGrammar in the process, exactly as the static does. Unlike PHP, Go compiles
// statements on many goroutines at once, so the flag is atomic to keep a concurrent toggle from being
// a data race. It stores "cascade disabled" so that its zero value is Laravel's default (cascade on).
var postgresTruncateWithoutCascade atomic.Bool

// CascadeOnTruncate mirrors PostgresGrammar::cascadeOnTruncate(): it turns the "cascade" keyword a
// truncate statement appends on or off. The default is on, matching Laravel. The flag is process-wide,
// so a caller that flips it should restore the previous value.
func CascadeOnTruncate(value bool) {
	postgresTruncateWithoutCascade.Store(!value)
}

// CompileTruncate mirrors PostgresGrammar::compileTruncate(): restart identity, then the cascade
// keyword unless CascadeOnTruncate(false) disabled it.
func (g *QueryGrammar) CompileTruncate(b core.Query) []core.Statement {
	sql := "truncate " + g.WrapTable(b.State().From) + " restart identity"
	if !postgresTruncateWithoutCascade.Load() {
		sql += " cascade"
	}

	return []core.Statement{{SQL: sql}}
}

// CompileLock mirrors PostgresGrammar::compileLock().
func (g *QueryGrammar) CompileLock(b core.Query, value any) string {
	if s, ok := value.(string); ok {
		return s
	}

	// Postgres spells a shared lock "for share" rather than MySQL's "lock in share mode".
	if locked, ok := value.(bool); ok && !locked {
		return "for share"
	}

	return "for update"
}

// CompileInsertOrIgnore mirrors PostgresGrammar::compileInsertOrIgnore(): the conflict clause is what
// ignores the row, so the statement is an ordinary insert with one appended.
func (g *QueryGrammar) CompileInsertOrIgnore(b core.Query, values []map[string]any) string {
	return g.CompileInsert(b, values) + " on conflict do nothing"
}

// CompileInsertOrIgnoreUsing mirrors PostgresGrammar::compileInsertOrIgnoreUsing().
func (g *QueryGrammar) CompileInsertOrIgnoreUsing(b core.Query, columns []any, sql string) string {
	return g.CompileInsertUsing(b, columns, sql) + " on conflict do nothing"
}

// PrepareBindingsForUpdate mirrors PostgresGrammar::prepareBindingsForUpdate(): array values and
// non-expression JSON selectors are encoded before binding. The assignments are grouped first so the
// bindings of a column's several JSON selectors travel together in the order the merged jsonb_set
// chain consumes them, keeping the placeholders aligned with the bindings exactly as
// CompileUpdateColumns emits them.
func (g *QueryGrammar) PrepareBindingsForUpdate(bindings map[string][]any, values core.UpdateValues) []any {
	merged := g.groupJsonColumnsForUpdate(values)
	out := make([]any, 0, len(merged))

	for _, value := range merged {
		out = g.appendUpdateBindings(out, value)
	}

	for _, typ := range core.BindingTypes {
		if typ == "select" {
			continue
		}
		out = append(out, bindings[typ]...)
	}

	return out
}

// appendUpdateBindings appends one (possibly merged) assignment's binding contribution. A JSON group
// contributes one binding per selector, in path order, each prepared by the rule a single selector's
// value takes.
func (g *QueryGrammar) appendUpdateBindings(out []any, value core.UpdateValue) []any {
	if group, ok := value.Value.(postgresJsonUpdate); ok {
		for _, selector := range group.values {
			out = g.appendUpdateBinding(out, value.Column, selector)
		}

		return out
	}

	return g.appendUpdateBinding(out, value.Column, value.Value)
}

// appendUpdateBinding applies the per-value rule: an array value and a non-expression JSON-selector
// value are json-encoded; anything else is appended as-is, so a sub-select splices in its own bindings.
func (g *QueryGrammar) appendUpdateBinding(out []any, column string, value any) []any {
	if _, subquery := value.(core.SubQueryBindings); subquery {
		return g.AppendUpdateValueBinding(out, value)
	}
	if core.IsArrayValue(value) || (g.IsJsonSelector(column) && !core.IsExpression(value)) {
		return append(out, grammars.EncodeJSON(value))
	}

	return g.AppendUpdateValueBinding(out, value)
}

// WrapJsonSelector mirrors PostgresGrammar::wrapJsonSelector(): native -> and ->> operators.
func (g *QueryGrammar) WrapJsonSelector(value string) string {
	segments := strings.Split(value, "->")
	field := g.Wrap(segments[0])

	attributes := g.WrapJsonPathAttributes(segments[1:], "'")
	if len(attributes) == 0 {
		return field
	}

	last := attributes[len(attributes)-1]
	rest := attributes[:len(attributes)-1]

	if len(rest) > 0 {
		return field + "->" + strings.Join(rest, "->") + "->>" + last
	}

	return field + "->>" + last
}

// WrapJsonPathAttributes mirrors PostgresGrammar::wrapJsonPathAttributes().
func (g *QueryGrammar) WrapJsonPathAttributes(path []string, quote string) []string {
	var out []string

	for _, attribute := range path {
		for _, key := range g.ParseJsonPathArrayKeys(attribute) {
			if _, err := strconv.Atoi(key); err == nil {
				out = append(out, key)
				continue
			}
			out = append(out, quote+key+quote)
		}
	}

	return out
}

// ParseJsonPathArrayKeys mirrors PostgresGrammar::parseJsonPathArrayKeys().
func (g *QueryGrammar) ParseJsonPathArrayKeys(attribute string) []string {
	parts := jsonPathArrayKeys.FindString(attribute)
	if parts == "" {
		return []string{attribute}
	}

	out := make([]string, 0, 2)
	if key := strings.TrimSuffix(attribute, parts); key != "" {
		out = append(out, key)
	}

	for _, match := range jsonPathKey.FindAllStringSubmatch(parts, -1) {
		if len(match) > 1 && match[1] != "" {
			out = append(out, match[1])
		}
	}

	return out
}

// CompileJsonContains mirrors PostgresGrammar::compileJsonContains().
func (g *QueryGrammar) CompileJsonContains(column string, value string) string {
	wrapped := strings.ReplaceAll(g.Wrap(column), "->>", "->")

	return "(" + wrapped + ")::jsonb @> " + value
}

// CompileJsonLength mirrors PostgresGrammar::compileJsonLength().
func (g *QueryGrammar) CompileJsonLength(column string, operator string, value string) string {
	wrapped := strings.ReplaceAll(g.Wrap(column), "->>", "->")

	return "jsonb_array_length((" + wrapped + ")::jsonb) " + operator + " " + value
}

// CompileJsonContainsKey mirrors PostgresGrammar::compileJsonContainsKey().
func (g *QueryGrammar) CompileJsonContainsKey(column string) string {
	segments := strings.Split(column, "->")
	last := segments[len(segments)-1]
	segments = segments[:len(segments)-1]

	index := ""
	if _, err := strconv.Atoi(last); err == nil {
		index = last
	} else if match := regexp.MustCompile(`\[(-?[0-9]+)\]$`).FindStringSubmatch(last); match != nil {
		segments = append(segments, strings.TrimSuffix(last, match[0]))
		index = match[1]
	}

	wrapped := strings.ReplaceAll(g.Wrap(strings.Join(segments, "->")), "->>", "->")

	if index != "" {
		n, _ := strconv.Atoi(index)
		if n < 0 {
			n = -n
		} else {
			n++
		}
		return "case when jsonb_typeof((" + wrapped + ")::jsonb) = 'array' then jsonb_array_length((" +
			wrapped + ")::jsonb) >= " + strconv.Itoa(n) + " else false end"
	}

	return "coalesce((" + wrapped + ")::jsonb ?? '" + strings.ReplaceAll(last, "'", "''") + "', false)"
}

// CompileJsonUpdateColumn mirrors PostgresGrammar::compileJsonUpdateColumn().
func (g *QueryGrammar) CompileJsonUpdateColumn(key string, value any) string {
	segments := strings.Split(key, "->")
	field := g.Wrap(segments[0])
	path := "'{" + strings.Join(g.WrapJsonPathAttributes(segments[1:], `"`), ",") + "}'"

	return field + " = jsonb_set(" + field + "::jsonb, " + path + ", " + g.Parameter(value) + ")"
}

// CompileRandom mirrors PostgresGrammar, which keeps the base RANDOM().
func (g *QueryGrammar) CompileRandom(seed any) string { return "RANDOM()" }

// validFullTextLanguages mirrors PostgresGrammar::validFullTextLanguages().
var validFullTextLanguages = []string{
	"simple", "arabic", "danish", "dutch", "english", "finnish", "french", "german",
	"hungarian", "indonesian", "irish", "italian", "lithuanian", "nepali", "norwegian",
	"portuguese", "romanian", "russian", "spanish", "swedish", "tamil", "turkish",
}

// WhereLike mirrors PostgresGrammar::whereLike(): the default match is case insensitive.
func (g *QueryGrammar) WhereLike(b core.Query, where core.Where) string {
	operator := ""
	if where.Not {
		operator = "not "
	}
	if where.CaseSensitive {
		operator += "like"
	} else {
		operator += "ilike"
	}

	basic := where
	basic.Operator = operator

	return g.WhereBasic(b, basic)
}

// WhereBitwise mirrors PostgresGrammar::whereBitwise().
func (g *QueryGrammar) WhereBitwise(b core.Query, where core.Where) string {
	operator := strings.ReplaceAll(where.Operator, "?", "??")

	return "(" + g.Wrap(where.Column) + " " + operator + " " + g.Parameter(where.Value) + ")::bool"
}

// WhereFullText mirrors PostgresGrammar::whereFullText().
func (g *QueryGrammar) WhereFullText(b core.Query, where core.Where) string {
	language := optionValue(where.Options, "language", "english")
	if !containsString(validFullTextLanguages, language) {
		language = "english"
	}

	columns := make([]string, 0, len(where.Columns))
	for _, column := range where.Columns {
		columns = append(columns, "to_tsvector('"+language+"', "+g.Wrap(column)+")")
	}

	mode := "plainto_tsquery"
	switch optionValue(where.Options, "mode", "") {
	case "phrase":
		mode = "phraseto_tsquery"
	case "websearch":
		mode = "websearch_to_tsquery"
	case "raw":
		mode = "to_tsquery"
	}

	return "(" + strings.Join(columns, " || ") + ") @@ " + mode + "('" + language + "', " + g.Parameter(where.Value) + ")"
}

// CompileHaving mirrors PostgresGrammar::compileHaving().
func (g *QueryGrammar) CompileHaving(b core.Query, having core.Having) string {
	if having.Type == core.HavingBitType {
		return "(" + g.Wrap(having.Column) + " " + having.Operator + " " + g.Parameter(having.Value) + ")::bool"
	}

	return g.BaseGrammar.CompileHaving(b, having)
}

// CompileUpdateColumns mirrors PostgresGrammar::compileUpdateColumns(): the key is reduced to its
// last dot-segment before wrapping (so a qualified update column assigns to the bare column), and JSON
// selectors use jsonb_set. The isJsonSelector test still runs against the full key, exactly as Laravel
// does, but the reduced column is what compileJsonUpdateColumn receives.
//
// Postgres rejects more than one assignment to the same column ("multiple assignments to same
// column"), so a column's JSON selectors are fused into one nested jsonb_set chain at the first
// selector's position: jsonb_set(jsonb_set(col::jsonb, path1, ?), path2, ?). Laravel leaves them as
// separate assignments, which only survives on a driver that permits the duplicate. Every other
// assignment keeps its position and its value.
func (g *QueryGrammar) CompileUpdateColumns(b core.Query, values core.UpdateValues) string {
	merged := g.groupJsonColumnsForUpdate(values)
	parts := make([]string, 0, len(merged))

	for _, value := range merged {
		if group, ok := value.Value.(postgresJsonUpdate); ok {
			parts = append(parts, g.compileJsonUpdateGroup(value.Column, group))
			continue
		}

		parts = append(parts, g.Wrap(core.LastDotSegment(value.Column))+" = "+g.Parameter(value.Value))
	}

	return strings.Join(parts, ", ")
}

// groupJsonColumnsForUpdate folds a column's several JSON selectors into a single assignment, kept in
// place at the first selector, so the compiled statement assigns the column once. A group carries the
// selector paths and values in order; a non-selector assignment is passed through untouched. The
// grouping is deterministic, so the compile and prepareBindingsForUpdate paths derive the same
// sequence and keep the placeholders aligned with the bindings.
func (g *QueryGrammar) groupJsonColumnsForUpdate(values core.UpdateValues) core.UpdateValues {
	merged := make(core.UpdateValues, 0, len(values))
	index := make(map[string]int, len(values))

	for _, value := range values {
		if !g.IsJsonSelector(value.Column) {
			merged = append(merged, value)
			continue
		}

		segments := strings.Split(core.LastDotSegment(value.Column), "->")
		field := segments[0]
		path := "'{" + strings.Join(g.WrapJsonPathAttributes(segments[1:], `"`), ",") + "}'"

		if i, ok := index[field]; ok {
			group := merged[i].Value.(postgresJsonUpdate)
			group.paths = append(group.paths, path)
			group.values = append(group.values, value.Value)
			merged[i].Value = group
			continue
		}

		index[field] = len(merged)
		merged = append(merged, core.UpdateValue{
			Column: core.LastDotSegment(value.Column),
			Value:  postgresJsonUpdate{paths: []string{path}, values: []any{value.Value}},
		})
	}

	return merged
}

// compileJsonUpdateGroup renders a column's JSON selectors as one assignment whose jsonb_set calls
// nest, so the column is assigned exactly once. A single selector renders identically to
// compileJsonUpdateColumn.
func (g *QueryGrammar) compileJsonUpdateGroup(column string, group postgresJsonUpdate) string {
	field := g.Wrap(strings.Split(column, "->")[0])

	expression := field + "::jsonb"
	for i, path := range group.paths {
		expression = "jsonb_set(" + expression + ", " + path + ", " + g.Parameter(group.values[i]) + ")"
	}

	return field + " = " + expression
}

// postgresJsonUpdate is one column's JSON selectors fused into a single chained jsonb_set assignment:
// the rendered path literals and their values, parallel and in selector order.
type postgresJsonUpdate struct {
	paths  []string
	values []any
}

// CompileUpdate mirrors PostgresGrammar::compileUpdate(): joins or a limit take the ctid rewrite.
func (g *QueryGrammar) CompileUpdate(b core.Query, values core.UpdateValues) string {
	if len(b.State().Joins) > 0 || b.State().Limit != nil {
		return g.compileUpdateWithJoinsOrLimit(b, values)
	}

	return g.BaseGrammar.CompileUpdate(b, values)
}

// compileUpdateWithJoinsOrLimit mirrors PostgresGrammar::compileUpdateWithJoinsOrLimit().
func (g *QueryGrammar) compileUpdateWithJoinsOrLimit(b core.Query, values core.UpdateValues) string {
	table := g.WrapTable(b.State().From)
	columns := g.CompileUpdateColumns(b, values)
	alias := grammars.AliasOf(b.State().From)

	inner := b.CloneForGrammar()
	inner.State().Columns = []any{alias + ".ctid"}
	selectSQL := g.CompileSelect(inner)

	return "update " + table + " set " + columns + " where " + g.Wrap("ctid") + " in (" + selectSQL + ")"
}

// CompileJoinLateral mirrors PostgresGrammar::compileJoinLateral(): Postgres supports lateral joins
// and spells them the same way MySQL does.
func (g *QueryGrammar) CompileJoinLateral(join core.Join, expression string) string {
	return strings.TrimSpace(join.JoinType() + " join lateral " + expression + " on true")
}

// CompileUpdateFrom mirrors PostgresGrammar::compileUpdateFrom().
func (g *QueryGrammar) CompileUpdateFrom(b core.Query, values core.UpdateValues) string {
	table := g.WrapTable(b.State().From)
	columns := g.CompileUpdateColumns(b, values)

	from := ""
	if len(b.State().Joins) > 0 {
		tables := make([]string, 0, len(b.State().Joins))
		for _, join := range b.State().Joins {
			tables = append(tables, g.WrapTable(join.JoinTable()))
		}
		if len(tables) > 0 {
			from = " from " + strings.Join(tables, ", ")
		}
	}

	where := g.CompileUpdateWheres(b)

	return strings.TrimSpace("update " + table + " set " + columns + from + " " + where)
}

// CompileUpdateWheres mirrors PostgresGrammar::compileUpdateWheres().
func (g *QueryGrammar) CompileUpdateWheres(b core.Query) string {
	baseWheres := g.CompileWheres(b)
	if len(b.State().Joins) == 0 {
		return baseWheres
	}

	joinWheres := g.CompileUpdateJoinWheres(b)
	if strings.TrimSpace(baseWheres) == "" {
		return "where " + grammars.RemoveLeadingBoolean(joinWheres)
	}

	return baseWheres + " " + joinWheres
}

// CompileUpdateJoinWheres mirrors PostgresGrammar::compileUpdateJoinWheres().
func (g *QueryGrammar) CompileUpdateJoinWheres(b core.Query) string {
	var parts []string

	for _, join := range b.State().Joins {
		for _, where := range join.State().Wheres {
			parts = append(parts, where.Boolean+" "+g.CompileWhere(b, where))
		}
	}

	return strings.Join(parts, " ")
}

// PrepareBindingsForUpdateFrom mirrors PostgresGrammar::prepareBindingsForUpdateFrom().
func (g *QueryGrammar) PrepareBindingsForUpdateFrom(bindings map[string][]any, values core.UpdateValues) []any {
	merged := g.groupJsonColumnsForUpdate(values)
	out := make([]any, 0, len(merged))

	for _, value := range merged {
		out = g.appendUpdateBindings(out, value)
	}
	out = append(out, bindings["where"]...)
	for _, typ := range core.BindingTypes {
		if typ == "select" || typ == "where" {
			continue
		}
		out = append(out, bindings[typ]...)
	}

	return out
}

// CompileDelete mirrors PostgresGrammar::compileDelete(): joins or a limit take the ctid rewrite.
func (g *QueryGrammar) CompileDelete(b core.Query) string {
	if len(b.State().Joins) > 0 || b.State().Limit != nil {
		table := g.WrapTable(b.State().From)
		alias := grammars.AliasOf(b.State().From)

		inner := b.CloneForGrammar()
		inner.State().Columns = []any{alias + ".ctid"}
		selectSQL := g.CompileSelect(inner)

		return "delete from " + table + " where " + g.Wrap("ctid") + " in (" + selectSQL + ")"
	}

	return g.BaseGrammar.CompileDelete(b)
}

// WrapJsonBooleanSelector mirrors PostgresGrammar::wrapJsonBooleanSelector().
func (g *QueryGrammar) WrapJsonBooleanSelector(value string) string {
	return "(" + strings.ReplaceAll(g.WrapJsonSelector(value), "->>", "->") + ")::jsonb"
}

// WrapJsonBooleanValue mirrors PostgresGrammar::wrapJsonBooleanValue().
func (g *QueryGrammar) WrapJsonBooleanValue(value string) string { return "'" + value + "'::jsonb" }

// SubstituteBindingsIntoRawSQL restores escaped PostgreSQL question-mark operators.
func (g *QueryGrammar) SubstituteBindingsIntoRawSQL(sql string, bindings []any) string {
	result := g.BaseGrammar.SubstituteBindingsIntoRawSQL(sql, bindings)
	for _, operator := range postgresOperators {
		if strings.Contains(operator, "?") {
			result = strings.ReplaceAll(result, strings.ReplaceAll(operator, "?", "??"), operator)
		}
	}
	return result
}

// optionValue reads a fulltext option. Go carries options as key or key:value strings.
func optionValue(options []string, key string, fallback string) string {
	for _, option := range options {
		if option == key {
			return key
		}
		if strings.HasPrefix(option, key+":") {
			return strings.TrimPrefix(option, key+":")
		}
	}

	return fallback
}

// containsString reports whether a slice contains a value.
func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}

	return false
}
