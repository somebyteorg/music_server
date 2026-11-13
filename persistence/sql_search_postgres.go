package persistence

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/sanitize"
	"github.com/navidrome/navidrome/utils/str"
)

var searchTerms = regexp.MustCompile(`"([^"]+)"|(\S+)`)

type postgresSearch struct {
	table string
	query string
}

func containsCJK(value string) bool {
	for _, ch := range value {
		if unicode.Is(unicode.Han, ch) || unicode.Is(unicode.Hiragana, ch) || unicode.Is(unicode.Katakana, ch) || unicode.Is(unicode.Hangul, ch) {
			return true
		}
	}
	return false
}

// newPostgresSearch builds a parameterized tsquery. Words use prefix matching;
// quoted phrases preserve adjacency. Operators in user input are ordinary words.
func newPostgresSearch(table, input string) searchStrategy {
	var terms []string
	for _, match := range searchTerms.FindAllStringSubmatch(input, -1) {
		phrase := match[1] != ""
		value := match[2]
		if phrase {
			value = match[1]
		}
		var tokens []string
		for word := range strings.FieldsSeq(value) {
			word = strings.ToLower(sanitize.Accents(strings.TrimSuffix(word, "*")))
			token := str.FTSPunctStrip.ReplaceAllString(word, "")
			if token == "" || token != word && utf8.RuneCountInString(token) <= 2 {
				return newLikeSearch(table, strings.ReplaceAll(input, `"`, ""))
			}
			term := "'" + token + "'"
			if !phrase {
				term += ":*"
			}
			tokens = append(tokens, term)
		}
		if len(tokens) > 0 {
			terms = append(terms, "("+strings.Join(tokens, " <-> ")+")")
		}
	}
	if len(terms) == 0 {
		return nil
	}
	return &postgresSearch{table: table, query: strings.Join(terms, " & ")}
}

func (s *postgresSearch) ToSql() (string, []any, error) {
	return s.table + ".search_document @@ to_tsquery('simple', ?)", []any{s.query}, nil
}

func (s *postgresSearch) execute(ctx context.Context, r sqlRepository, query SelectBuilder, dest any, cfg searchConfig) error {
	query = clearOrder(query).Where(s).
		OrderByClause(Expr("ts_rank_cd("+s.table+".search_document, to_tsquery('simple', ?)) DESC", s.query)).
		OrderBy(cfg.OrderBy...).OrderBy(s.table + ".id")
	return r.queryAll(ctx, query, dest)
}
