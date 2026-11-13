package persistence

import (
	"fmt"
	"regexp"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/lann/builder"
)

type cursorOrder struct {
	expression string
	alias      string
	descending bool
	nullsFirst bool
	notNull    bool
}

var projectionAlias = regexp.MustCompile(`(?is)^(.+?)\s+(?:as\s+)?([a-z_][a-z_0-9]*)$`)

// cursorOrders resolves output aliases to their source expressions before adding
// sort-key projections. In particular, annotation defaults must sort as zero or
// false, exactly as they do in the original SELECT.
func (r sqlRepository) cursorOrders(query SelectBuilder, key string) ([]cursorOrder, error) {
	aliases := map[string]string{}
	if columns, ok := builder.Get(query, "Columns"); ok {
		for _, column := range columns.([]Sqlizer) {
			sql, args, err := column.ToSql()
			if err != nil {
				return nil, err
			}
			if match := projectionAlias.FindStringSubmatch(sql); len(match) > 0 && len(args) == 0 {
				aliases[strings.ToLower(match[2])] = match[1]
			}
			if _, annotation := annotationColumns()[sql]; annotation {
				aliases[sql] = "annotation." + sql
			}
		}
	}
	var result []cursorOrder
	if parts, ok := builder.Get(query, "OrderByParts"); ok {
		for _, part := range parts.([]Sqlizer) {
			sql, args, err := part.ToSql()
			if err != nil {
				return nil, err
			}
			if len(args) > 0 {
				return nil, fmt.Errorf("cursor sort expressions must not contain bind parameters")
			}
			for _, expression := range strings.FieldsFunc(sql, splitFunc(',')) {
				order := cursorOrder{expression: strings.TrimSpace(expression)}
				explicitNulls := false
				for _, suffix := range []string{" NULLS FIRST", " NULLS LAST"} {
					if strings.HasSuffix(strings.ToUpper(order.expression), suffix) {
						order.expression = strings.TrimSpace(order.expression[:len(order.expression)-len(suffix)])
						order.nullsFirst, explicitNulls = suffix == " NULLS FIRST", true
						break
					}
				}
				for _, suffix := range []string{" DESC", " ASC"} {
					if strings.HasSuffix(strings.ToUpper(order.expression), suffix) {
						order.expression = strings.TrimSpace(order.expression[:len(order.expression)-len(suffix)])
						order.descending = suffix == " DESC"
						break
					}
				}
				if !explicitNulls {
					order.nullsFirst = order.descending
				}
				order.expression = r.resolveCursorAliases(order.expression, aliases)
				result = append(result, order)
			}
		}
	}
	// Primary keys give every page a strict order, including equal sort values.
	if len(result) == 0 || result[len(result)-1].expression != key {
		result = append(result, cursorOrder{expression: key})
	}
	for i := range result {
		result[i].alias = fmt.Sprintf("_cursor_sort_%d", i)
		result[i].notNull = result[i].expression == key
	}
	return result, nil
}

func (r sqlRepository) resolveCursorAliases(expression string, aliases map[string]string) string {
	var result strings.Builder
	for i := 0; i < len(expression); {
		start := i
		switch ch := expression[i]; {
		case ch == '\'' || ch == '"':
			i++
			for i < len(expression) {
				if expression[i] == ch {
					i++
					if i < len(expression) && expression[i] == ch {
						i++
						continue
					}
					break
				}
				i++
			}
		case ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z':
			for i < len(expression) && (expression[i] == '_' || expression[i] >= 'a' && expression[i] <= 'z' || expression[i] >= 'A' && expression[i] <= 'Z' || expression[i] >= '0' && expression[i] <= '9') {
				i++
			}
			if start == 0 || expression[start-1] != '.' {
				name := strings.ToLower(expression[start:i])
				if replacement, ok := aliases[name]; ok {
					result.WriteString("(" + replacement + ")")
					continue
				}
				if r.isFieldWhiteListed != nil && r.isFieldWhiteListed(name) && (i == len(expression) || expression[i] != '.') {
					result.WriteString(r.tableName + "." + name)
					continue
				}
			}
		default:
			i++
		}
		result.WriteString(expression[start:i])
	}
	return result.String()
}

func (o cursorOrder) sqlOrder() string {
	direction, nulls := "ASC", "NULLS LAST"
	if o.descending {
		direction = "DESC"
	}
	if o.nullsFirst {
		nulls = "NULLS FIRST"
	}
	return o.alias + " " + direction + " " + nulls
}

// cursorRanges separates the disjoint ranges after a boundary, including NULL
// groups, so each range can use an index instead of one wide OR filter.
func cursorRanges(orders []cursorOrder, values []any) []Sqlizer {
	var ranges []Sqlizer
	prefix := And{}
	add := func(next Sqlizer) {
		branch := append(And(nil), prefix...)
		ranges = append(ranges, append(branch, next))
	}
	for i, order := range orders {
		column, value := order.alias, values[i]
		if value == nil {
			if order.nullsFirst {
				add(NotEq{column: nil})
			}
		} else {
			operator := ">"
			if order.descending {
				operator = "<"
			}
			add(Expr(column+" "+operator+" ?", value))
			if !order.nullsFirst && !order.notNull {
				add(Eq{column: nil})
			}
		}
		prefix = append(prefix, Eq{column: value})
	}
	return ranges
}
