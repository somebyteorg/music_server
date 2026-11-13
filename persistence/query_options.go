package persistence

import (
	"context"
	"fmt"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/hasher"
	"github.com/zeebo/xxh3"
)

// setSortMappings sets the mappings for the sort fields. If the sort field is not in the map, it will be used as is.
// This applies per comma-separated part, so a key added here also defines that bare name wherever a
// caller uses it inside a sort list.
//
// If PreferSortTags is enabled, it will map the order fields to the corresponding sort expression,
// which gives precedence to sort tags.
// Ex: order_title => lower(coalesce(nullif(sort_title,”),order_title))
// To avoid performance issues, indexes should be created for these sort expressions
//
// NOTE: if an individual item has spaces, it should be wrapped in parentheses. For example,
// you should write "(lyrics != '[]')". This prevents the item being split unexpectedly.
// Without parentheses, "lyrics != '[]'" would be mapped as simply "lyrics"
func (r *sqlRepository) setSortMappings(mappings map[string]string, tableName ...string) {
	tn := r.tableName
	if len(tableName) > 0 {
		tn = tableName[0]
	}
	for k, v := range mappings {
		mappings[k] = mapSortOrder(tn, v)
	}
	r.sortMappings = mappings
}

func (r sqlRepository) newSelect(ctx context.Context, options ...model.QueryOptions) SelectBuilder {
	sq := Select().From(r.tableName)
	if len(options) > 0 {
		r.resetSeededRandom(ctx, options)
		sq = r.applyOptions(sq, options...)
		sq = r.applyFilters(sq, options...)
	}
	return sq
}

func (r sqlRepository) applyOptions(sq SelectBuilder, options ...model.QueryOptions) SelectBuilder {
	if len(options) > 0 {
		if options[0].Max > 0 {
			sq = sq.Limit(uint64(options[0].Max))
		}
		if options[0].Offset > 0 {
			sq = sq.Offset(uint64(options[0].Offset))
		}
		if options[0].Sort != "" {
			sort, order := options[0].Sort, options[0].Order
			// Per-key directions are absolute, so buildSortOrder gets an ascending baseline.
			// resetSeededRandom has already selected a deterministic PostgreSQL hash order.
			if len(options[0].SortOrders) > 0 && sort != "random" && !strings.HasPrefix(sort, "hashtextextended(") {
				sort = r.sortWithKeyOrders(sort, options[0].SortOrders)
				order = ""
			}
			sq = sq.OrderBy(r.buildSortOrder(sort, order)).OrderBy(r.tableName + ".id")
		}
	}
	return sq
}

// TODO Change all sortMappings to have a consistent case
func (r sqlRepository) sortMapping(sort string) string {
	if mapping, _, ok := r.lookupSortMapping(sort); ok {
		return mapping
	}
	// Each part of a comma list is resolved on its own, so a mix of mapped keys and plain columns
	// keeps the mappings the recognized parts have.
	parts := strings.FieldsFunc(sort, splitFunc(','))
	mapped := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if partMapping, _, ok := r.lookupSortMapping(part); ok {
			part = partMapping
		} else {
			part = toSnakeCase(part)
		}
		mapped = append(mapped, part)
	}
	return strings.Join(mapped, ", ")
}

// lookupSortMapping also returns the snake_case form when it had to derive one, so a caller's
// fallback doesn't recompute it: toSnakeCase runs two regexps.
func (r sqlRepository) lookupSortMapping(sort string) (mapping, snakeCased string, ok bool) {
	if mapping, ok = r.sortMappings[sort]; ok {
		return mapping, sort, true
	}
	if mapping, ok = r.sortMappings[toCamelCase(sort)]; ok {
		return mapping, "", true
	}
	snakeCased = toSnakeCase(sort)
	mapping, ok = r.sortMappings[snakeCased]
	return mapping, snakeCased, ok
}

// sortWithKeyOrders maps each logical sort key, then marks descending keys so buildSortOrder
// keeps that direction on every column the mapping expands into.
func (r sqlRepository) sortWithKeyOrders(sort string, orders []string) string {
	keys := strings.FieldsFunc(sort, splitFunc(','))
	out := make([]string, 0, len(keys))
	for i, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		mapped := r.sortMapping(key)
		if !descendingOrder(i, orders) {
			out = append(out, mapped)
			continue
		}
		for _, col := range strings.FieldsFunc(mapped, splitFunc(',')) {
			out = append(out, strings.TrimSpace(col)+" desc")
		}
	}
	return strings.Join(out, ", ")
}

func descendingOrder(i int, orders []string) bool {
	if len(orders) == 0 {
		return false
	}
	raw := orders[0]
	if i < len(orders) {
		raw = orders[i]
	}
	return raw == "desc"
}

func (r sqlRepository) buildSortOrder(sort, order string) string {
	sort = r.sortMapping(sort)
	order = strings.ToLower(strings.TrimSpace(order))
	var reverseOrder string
	if order == "desc" {
		reverseOrder = "asc"
	} else {
		order = "asc"
		reverseOrder = "desc"
	}

	parts := strings.FieldsFunc(sort, splitFunc(','))
	newSort := make([]string, 0, len(parts))
	for _, p := range parts {
		f := strings.FieldsFunc(p, splitFunc(' '))
		newField := make([]string, 1, len(f))
		newField[0] = f[0]
		if len(f) == 1 {
			newField = append(newField, order)
		} else {
			if f[1] == "asc" {
				newField = append(newField, order)
			} else {
				newField = append(newField, reverseOrder)
			}
		}
		newSort = append(newSort, strings.Join(newField, " "))
	}
	return strings.Join(newSort, ", ")
}

func splitFunc(delimiter rune) func(c rune) bool {
	open := 0
	return func(c rune) bool {
		if c == '(' {
			open++
			return false
		}
		if open > 0 {
			if c == ')' {
				open--
			}
			return false
		}
		return c == delimiter
	}
}

func (r sqlRepository) seedKey(ctx context.Context) string {
	// Keep one random seed per entity type and user.
	userIDHash := xxh3.Hash([]byte(loggedUser(ctx).ID))
	return fmt.Sprintf("%s|%016x", r.tableName, userIDHash)
}

func (r sqlRepository) resetSeededRandom(ctx context.Context, options []model.QueryOptions) {
	if len(options) == 0 || options[0].Sort != "random" {
		return
	}
	key := r.seedKey(ctx)
	if options[0].Seed != "" {
		hasher.SetSeed(key, options[0].Seed)
	} else if options[0].Offset == 0 {
		hasher.Reseed(key)
	}
	seed := int64(xxh3.HashString(hasher.CurrentSeed(key)))
	options[0].Sort = fmt.Sprintf("hashtextextended(CAST(%s.id AS text), %d)", r.tableName, seed)
}
