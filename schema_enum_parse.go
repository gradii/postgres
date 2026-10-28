package postgres

import "strings"

// enumMembership accepts only a single column's string-membership predicate.
// It deliberately rejects functions, arithmetic, AND, and non-text casts.
func enumMembership(expression, column string) bool {
	tokens, ok := lexEnumCheck(expression)
	if !ok {
		return false
	}
	p := enumCheckParser{tokens: tokens, column: column}
	return p.expression(0) && p.pos == len(tokens)
}

type enumToken struct {
	kind byte
	text string
}
type enumCheckParser struct {
	tokens []enumToken
	pos    int
	column string
}

func lexEnumCheck(sql string) ([]enumToken, bool) {
	var tokens []enumToken
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		escaped := false
		if (c == 'N' || c == 'n' || c == 'E' || c == 'e') && i+1 < len(sql) && sql[i+1] == '\'' {
			escaped = c == 'E' || c == 'e'
			i++
			c = sql[i]
		}
		if c == '\'' || c == '"' {
			end, kind := c, byte('i')
			if c == '\'' {
				kind = 's'
			}
			if c == '[' {
				end = ']'
			}
			i++
			var value strings.Builder
			closed := false
			for i < len(sql) {
				if escaped && sql[i] == '\\' && i+1 < len(sql) {
					value.WriteByte(sql[i])
					value.WriteByte(sql[i+1])
					i += 2
					continue
				}
				if sql[i] == end {
					if i+1 < len(sql) && sql[i+1] == end {
						value.WriteByte(end)
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				value.WriteByte(sql[i])
				i++
			}
			if !closed {
				return nil, false
			}
			tokens = append(tokens, enumToken{kind, value.String()})
			continue
		}
		if strings.ContainsRune("()[],=:", rune(c)) {
			tokens = append(tokens, enumToken{'p', string(c)})
			i++
			continue
		}
		start := i
		for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z' || sql[i] >= '0' && sql[i] <= '9' || sql[i] == '_' || sql[i] == '$' || sql[i] >= 128) {
			i++
		}
		if start == i {
			return nil, false
		}
		tokens = append(tokens, enumToken{'w', sql[start:i]})
	}
	return tokens, true
}
func (p *enumCheckParser) take(value string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == 'p' && p.tokens[p.pos].text == value {
		p.pos++
		return true
	}
	return false
}
func (p *enumCheckParser) word(value string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == 'w' && strings.EqualFold(p.tokens[p.pos].text, value) {
		p.pos++
		return true
	}
	return false
}
func (p *enumCheckParser) casts() bool {
	for p.take(":") {
		if !p.take(":") {
			return false
		}
		if p.word("character") {
			if !p.word("varying") {
				return false
			}
		} else if !p.word("text") && !p.word("varchar") {
			return false
		}
		if p.take("[") && !p.take("]") {
			return false
		}
	}
	return true
}
func (p *enumCheckParser) scalar(kind byte, depth int) bool {
	if depth > 64 {
		return false
	}
	start := p.pos
	if p.take("(") {
		if !p.scalar(kind, depth+1) || !p.take(")") {
			p.pos = start
			return false
		}
	} else {
		if p.pos >= len(p.tokens) {
			return false
		}
		token := p.tokens[p.pos]
		if kind == 's' {
			if token.kind != 's' {
				return false
			}
		} else if token.kind != 'i' && token.kind != 'w' || token.text != p.column {
			return false
		}
		p.pos++
	}
	if !p.casts() {
		p.pos = start
		return false
	}
	return true
}
func (p *enumCheckParser) array(depth int) bool {
	if depth > 64 {
		return false
	}
	if p.take("(") {
		if !p.array(depth+1) || !p.take(")") {
			return false
		}
	} else {
		if !p.word("array") || !p.take("[") || !p.scalar('s', depth+1) {
			return false
		}
		for p.take(",") {
			if !p.scalar('s', depth+1) {
				return false
			}
		}
		if !p.take("]") {
			return false
		}
	}
	return p.casts()
}
func (p *enumCheckParser) term(depth int) bool {
	if depth > 64 {
		return false
	}
	start := p.pos
	if p.scalar('i', depth+1) {
		if p.take("=") {
			if p.word("any") {
				return p.take("(") && p.array(depth+1) && p.take(")")
			}
			return p.scalar('s', depth+1)
		}
		if p.word("in") {
			if !p.take("(") || !p.scalar('s', depth+1) {
				return false
			}
			for p.take(",") {
				if !p.scalar('s', depth+1) {
					return false
				}
			}
			return p.take(")")
		}
	}
	p.pos = start
	return p.take("(") && p.expression(depth+1) && p.take(")")
}
func (p *enumCheckParser) expression(depth int) bool {
	if depth > 64 || !p.term(depth+1) {
		return false
	}
	for p.word("or") {
		if !p.term(depth + 1) {
			return false
		}
	}
	return true
}
