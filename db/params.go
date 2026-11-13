package db

import (
	"fmt"
	"strings"
)

// bindParams rewrites builder placeholders while preserving quoted SQL and comments.
// A doubled question mark represents a literal PostgreSQL JSON operator.
func bindParams(query string, placeholder func(int) string) (string, int, error) {
	var out strings.Builder
	count := 0
	for i := 0; i < len(query); {
		start := i
		switch {
		case query[i] == '\'' || query[i] == '"':
			quote := query[i]
			i++
			closed := false
			for i < len(query) {
				if query[i] == quote {
					i++
					if i < len(query) && query[i] == quote {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", 0, fmt.Errorf("unterminated SQL quote")
			}
		case strings.HasPrefix(query[i:], "--"):
			if end := strings.IndexByte(query[i:], '\n'); end >= 0 {
				i += end + 1
			} else {
				i = len(query)
			}
		case strings.HasPrefix(query[i:], "/*"):
			i += 2
			depth := 1
			for i < len(query) && depth > 0 {
				if strings.HasPrefix(query[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(query[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return "", 0, fmt.Errorf("unterminated SQL comment")
			}
		case query[i] == '$':
			end := i + 1
			for end < len(query) && (query[end] == '_' || query[end] >= 'a' && query[end] <= 'z' || query[end] >= 'A' && query[end] <= 'Z' || end > i+1 && query[end] >= '0' && query[end] <= '9') {
				end++
			}
			if end < len(query) && query[end] == '$' {
				tag := query[i : end+1]
				closeAt := strings.Index(query[end+1:], tag)
				if closeAt < 0 {
					return "", 0, fmt.Errorf("unterminated SQL dollar quote")
				}
				i = end + 1 + closeAt + len(tag)
			} else {
				i++
			}
		case query[i] == '?':
			if i+1 < len(query) && query[i+1] == '?' {
				out.WriteByte('?')
				i += 2
			} else {
				out.WriteString(placeholder(count))
				count++
				i++
			}
			continue
		default:
			i++
		}
		out.WriteString(query[start:i])
	}
	return out.String(), count, nil
}

// NamedParams binds Squirrel statements to the named parameters used by repositories.
func NamedParams(statement string, args []any) (string, map[string]any, error) {
	query, count, err := bindParams(statement, func(i int) string { return fmt.Sprintf("{:p%d}", i) })
	if err != nil {
		return "", nil, err
	}
	if count != len(args) {
		return "", nil, fmt.Errorf("SQL parameter count: got %d arguments for %d placeholders", len(args), count)
	}
	params := make(map[string]any, len(args))
	for i, value := range args {
		params[fmt.Sprintf("p%d", i)] = value
	}
	return query, params, nil
}

// PositionalParams binds direct SQL statements to PostgreSQL's numbered parameters.
func PositionalParams(statement string, args []any) (string, []any, error) {
	query, count, err := bindParams(statement, func(i int) string { return fmt.Sprintf("$%d", i+1) })
	if err != nil {
		return "", nil, err
	}
	if count != len(args) {
		return "", nil, fmt.Errorf("SQL parameter count: got %d arguments for %d placeholders", len(args), count)
	}
	return query, args, nil
}
