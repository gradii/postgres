package postgres

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gradii/framework/errors"
)

// Escape formats a PostgreSQL scalar literal for SQL previews. Query execution uses
// bound parameters, independently of this presentation-only operation.
func (d *Dialect) Escape(value any, binary bool) (string, error) {
	if value == nil {
		return "null", nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return "null", nil
	}
	if valuer, ok := value.(driver.Valuer); ok {
		scalar, err := valuer.Value()
		if err != nil {
			return "", err
		}
		return d.Escape(scalar, binary)
	}
	if reflected.Kind() == reflect.Pointer {
		return d.Escape(reflected.Elem().Interface(), binary)
	}
	if date, ok := value.(time.Time); ok {
		value = date.Format("2006-01-02 15:04:05")
		reflected = reflect.ValueOf(value)
	}
	if binary {
		var bytes []byte
		switch input := value.(type) {
		case []byte:
			bytes = input
		case string:
			bytes = []byte(input)
		default:
			return "", errors.DatabaseUnsupportedType.Args(fmt.Sprintf("%T", value), "string or []byte")
		}
		return "'\\x" + hex.EncodeToString(bytes) + "'::bytea", nil
	}
	switch reflected.Kind() {
	case reflect.Bool:
		if reflected.Bool() {
			return "true", nil
		}
		return "false", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(reflected.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(reflected.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		number := reflected.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return "", errors.DatabaseEscapeNonFinite
		}
		return strconv.FormatFloat(number, 'g', -1, reflected.Type().Bits()), nil
	}
	var text string
	switch input := value.(type) {
	case []byte:
		text = string(input)
	default:
		if reflected.Kind() != reflect.String {
			return "", errors.DatabaseUnsupportedType.Args(fmt.Sprintf("%T", value), "scalar or []byte")
		}
		text = reflected.String()
	}
	if !utf8.ValidString(text) {
		return "", errors.DatabaseEscapeInvalidUTF8
	}
	if strings.IndexByte(text, 0) >= 0 {
		return "", errors.DatabaseEscapeNullByte
	}
	return "'" + strings.ReplaceAll(text, "'", "''") + "'", nil
}
