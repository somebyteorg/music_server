package persistence

import (
	"context"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/utils/str"
)

func formatFullText(text ...string) string { return " " + str.SanitizeStrings(text...) }

type searchConfig struct {
	NaturalOrder string
	OrderBy      []string
	MBIDFields   []string
}

type searchStrategy interface {
	Sqlizer
	execute(context.Context, sqlRepository, SelectBuilder, any, searchConfig) error
}

func getSearchStrategy(table, query string) searchStrategy {
	if conf.Server.Search.Backend == "legacy" || conf.Server.Search.FullString {
		return newLegacySearch(table, query)
	}
	if containsCJK(query) {
		return newLikeSearch(table, query)
	}
	return newPostgresSearch(table, query)
}

func (r sqlRepository) doSearch(ctx context.Context, query SelectBuilder, text string, results any, cfg searchConfig) error {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "*"))
	query = query.Where(Eq{r.tableName + ".missing": false})
	if text == "" || text == `""` {
		return r.queryAll(ctx, clearOrder(query).OrderBy(cfg.NaturalOrder), results)
	}
	if uuid.Validate(text) == nil && len(cfg.MBIDFields) > 0 {
		return r.queryAll(ctx, query.Where(mbidExpr(r.tableName, text, cfg.MBIDFields...)), results)
	}
	if len(text) < 2 {
		return nil
	}
	strategy := getSearchStrategy(r.tableName, text)
	if strategy == nil {
		return nil
	}
	return strategy.execute(ctx, r, query, results, cfg)
}

func mbidExpr(table, mbid string, fields ...string) Sqlizer {
	if uuid.Validate(mbid) != nil || len(fields) == 0 {
		return nil
	}
	cond := Or{}
	for _, field := range fields {
		cond = append(cond, Eq{table + "." + field: strings.ToLower(mbid)})
	}
	return cond
}
