package persistence

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/lann/builder"
	"github.com/navidrome/navidrome/model"
	id2 "github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils/slice"
	"github.com/pocketbase/dbx"
)

// sqlRepository is the base repository for all SQL repositories. It provides common functions to interact with the DB.
// When creating a new repository using this base, you must:
//
//   - Embed this struct.
//   - Set the db field.
//   - Call registerModel with the model instance and any possible filters.
//   - If the model has a different table name than the default (lowercase of the model name), it should be set manually
//     using the tableName field.
//   - Sort mappings must be set with setSortMappings method. If a sort field is not in the map, it will be used as the name of the column.
//
// All fields in filters and sortMappings must be in snake_case. Only sorts and filters based on real field names or
// defined in the mappings will be allowed.
type sqlRepository struct {
	tableName string
	db        dbx.Builder

	// Do not set these fields manually, they are set by the registerModel method
	filterMappings     map[string]filterFunc
	isFieldWhiteListed fieldWhiteListedFunc
	// Do not set this field manually, it is set by the setSortMappings method
	sortMappings map[string]string
}

const invalidUserId = "-1"

func loggedUser(ctx context.Context) *model.User {
	if user, ok := request.UserFrom(ctx); !ok {
		return &model.User{ID: invalidUserId}
	} else {
		return &user
	}
}

// ownerFilter returns the predicate restricting access to rows owned by the logged-in user, for
// tables with a user_id column. It returns nil for admins and for headless/system contexts (invalid
// user), meaning "no ownership restriction". Callers should skip the WHERE clause when it is nil.
//
// The predicate uses an unqualified user_id, so it only works on queries where that column is
// unambiguous (no join introducing a second user_id).
func (r sqlRepository) ownerFilter(ctx context.Context) Sqlizer {
	if usr := loggedUser(ctx); !usr.IsAdmin && usr.ID != invalidUserId {
		return Eq{"user_id": usr.ID}
	}
	return nil
}

// addRestriction combines an optional caller predicate with the ownership filter, producing the
// WHERE clause for owner-scoped reads. For admins and headless contexts ownerFilter() is nil and
// only the caller's predicate (if any) remains.
func (r sqlRepository) addRestriction(ctx context.Context, sql ...Sqlizer) Sqlizer {
	s := And{}
	if len(sql) > 0 {
		s = append(s, sql[0])
	}
	if owner := r.ownerFilter(ctx); owner != nil {
		s = append(s, owner)
	}
	return s
}

// writeAccess says who may change a row in a table with a user_id column.
type writeAccess int

const (
	ownerOrAdmin writeAccess = iota // admins may write any row
	ownerOnly                       // even admins may only write their own rows
)

// ownedRow matches the row rowID only if the logged-in user may write it under access.
func (r sqlRepository) ownedRow(ctx context.Context, rowID string, access writeAccess) Sqlizer {
	if access == ownerOnly {
		return And{Eq{"id": rowID}, Eq{"user_id": loggedUser(ctx).ID}}
	}
	return r.addRestriction(ctx, Eq{"id": rowID})
}

func (r *sqlRepository) registerModel(instance any, filters map[string]filterFunc) {
	if r.tableName == "" {
		r.tableName = strings.TrimPrefix(reflect.TypeOf(instance).String(), "*model.")
		r.tableName = toSnakeCase(r.tableName)
	}
	r.tableName = strings.ToLower(r.tableName)
	r.isFieldWhiteListed = registerModelWhiteList(instance)
	r.filterMappings = filters
}

func (r sqlRepository) applyFilters(sq SelectBuilder, options ...model.QueryOptions) SelectBuilder {
	if len(options) > 0 && options[0].Filters != nil {
		sq = sq.Where(options[0].Filters)
	}
	return sq
}

// libraryIdFilter is a filter function to be added to resources that have a library_id column.
func libraryIdFilter(_ string, value any) Sqlizer {
	return Eq{"library_id": value}
}

// applyLibraryFilter adds library filtering to queries for tables that have a library_id column
// This ensures users only see content from libraries they have access to
func (r sqlRepository) applyLibraryFilter(ctx context.Context, sq SelectBuilder, tableName ...string) SelectBuilder {
	user := loggedUser(ctx)

	// Background tasks have no personal selection; authenticated users share the same filter.
	if user.ID == invalidUserId || user.HasAllLibraryAccess() {
		return sq
	}

	table := r.tableName
	if len(tableName) > 0 {
		table = tableName[0]
	}

	// Get user's accessible library IDs
	// Use subquery to filter by user's library access
	return sq.Where(Expr(table+".library_id IN ("+
		"SELECT ul.library_id FROM user_library ul WHERE ul.user_id = ?)", user.ID))
}

func (r sqlRepository) exists(ctx context.Context, cond Sqlizer) (bool, error) {
	existsQuery := Select().Column(Alias(Exists(r.tableName, cond), "exist"))
	var res struct{ Exist bool }
	err := r.queryOne(ctx, existsQuery, &res)
	return res.Exist, err
}

// updateOwned performs an atomic, ownership-restricted update of the row identified by id, for
// repositories whose table has a user_id column. Non-admins can only update rows they own: the
// ownership predicate is part of the UPDATE's WHERE clause, so a row owned by another user simply
// does not match and no write happens. Ownership itself is immutable here: user_id is never written,
// so no caller (admin included) can reassign a row to a different owner via an update. Unlike put,
// it never falls through to an INSERT, so a non-matching id never creates a row.
//
// When the update matches no row it classifies the failure: if the row exists but is owned by
// another user it returns rest.ErrPermissionDenied, otherwise rest.ErrNotFound. The write itself is
// still atomic; the extra lookup happens only on the failure path (count == 0), where no write
// occurred, so there is no TOCTOU on the update.
func (r sqlRepository) updateOwned(ctx context.Context, id string, m any, colsToUpdate ...string) error {
	values, err := toSQLArgs(m)
	if err != nil {
		return fmt.Errorf("error preparing values to write to DB: %w", err)
	}
	updateValues := filterUpdateValues(values, id, colsToUpdate...)
	delete(updateValues, "user_id") // ownership is immutable on update
	return r.updateOwnedRow(ctx, id, ownerOrAdmin, updateValues)
}

// updateOwnedRow sets values on the row rowID if the logged-in user may write it under access.
func (r sqlRepository) updateOwnedRow(ctx context.Context, rowID string, access writeAccess, values map[string]any) error {
	return r.runRowWrite(ctx, rowID, Update(r.tableName).SetMap(values).Where(r.ownedRow(ctx, rowID, access)))
}

// deleteOwned performs an atomic, ownership-restricted delete of the row identified by id, for
// repositories whose table has a user_id column. Non-admins can only delete rows they own: the
// ownership predicate is part of the DELETE's WHERE clause, so a row owned by another user simply
// does not match and is left untouched. The failure path mirrors updateOwned (see
// classifyOwnedWriteMiss), so there is no TOCTOU on the delete.
func (r sqlRepository) deleteOwned(ctx context.Context, id string) error {
	return r.runRowWrite(ctx, id, Delete(r.tableName).Where(r.ownedRow(ctx, id, ownerOrAdmin)))
}

// runRowWrite executes q, a write already filtered by ownedRow(rowID, …), and classifies a miss.
func (r sqlRepository) runRowWrite(ctx context.Context, rowID string, q Sqlizer) error {
	count, err := r.executeSQL(ctx, q)
	if err != nil {
		return err
	}
	if count == 0 {
		return r.classifyOwnedWriteMiss(ctx, rowID)
	}
	return nil
}

func (r sqlRepository) deleteOwnedAll(ctx context.Context, ids ...string) error {
	for _, id := range ids {
		if err := r.deleteOwned(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// classifyOwnedWriteMiss explains why an ownership-filtered write (updateOwned/deleteOwned) matched
// no row: rest.ErrPermissionDenied if the row exists but is owned by another user, otherwise
// rest.ErrNotFound. It runs only on the failure path (count == 0), where no write occurred.
func (r sqlRepository) classifyOwnedWriteMiss(ctx context.Context, id string) error {
	exists, err := r.exists(ctx, Eq{"id": id})
	if err != nil {
		return err
	}
	if exists {
		return rest.ErrPermissionDenied
	}
	return rest.ErrNotFound
}

func (r sqlRepository) count(ctx context.Context, countQuery SelectBuilder, options ...model.QueryOptions) (int64, error) {
	countQuery = clearOrder(builder.Delete(countQuery, "GroupBys").(SelectBuilder)).
		RemoveColumns().Columns("count(distinct " + r.tableName + ".id) as count").
		RemoveOffset().RemoveLimit().
		From(r.tableName)
	countQuery = r.applyFilters(countQuery, options...)
	var res struct{ Count int64 }
	err := r.queryOne(ctx, countQuery, &res)
	return res.Count, err
}

func (r sqlRepository) putByMatch(ctx context.Context, filter Sqlizer, id string, m any, colsToUpdate ...string) (string, error) {
	if id != "" {
		return r.put(ctx, id, m, colsToUpdate...)
	}
	existsQuery := r.newSelect(ctx).Columns("id").From(r.tableName).Where(filter)

	var res struct{ ID string }
	err := r.queryOne(ctx, existsQuery, &res)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return "", err
	}
	return r.put(ctx, res.ID, m, colsToUpdate...)
}

// selectUpdateColumns keeps only the requested colsToUpdate (or all columns when none are
// specified), dropping columns that must never be overwritten on update (created_at, birth_time).
func selectUpdateColumns(values map[string]any, colsToUpdate ...string) map[string]any {
	updateValues := map[string]any{}

	c2upd := slice.ToMap(colsToUpdate, func(s string) (string, struct{}) {
		return toSnakeCase(s), struct{}{}
	})
	for k, v := range values {
		if _, found := c2upd[k]; len(c2upd) == 0 || found {
			updateValues[k] = v
		}
	}

	delete(updateValues, "created_at")
	// To avoid updating the media_file birth_time on each scan. Not the best solution, but it works for now
	// TODO move to mediafile_repository when each repo has its own upsert method
	delete(updateValues, "birth_time")
	return updateValues
}

func filterUpdateValues(values map[string]any, id string, colsToUpdate ...string) map[string]any {
	updateValues := selectUpdateColumns(values, colsToUpdate...)
	updateValues["id"] = id
	return updateValues
}

func (r sqlRepository) put(ctx context.Context, id string, m any, colsToUpdate ...string) (newId string, err error) {
	values, err := toSQLArgs(m)
	if err != nil {
		return "", fmt.Errorf("error preparing values to write to DB: %w", err)
	}
	if id == "" {
		id = id2.NewRandom()
	}
	values["id"] = id
	updates := selectUpdateColumns(values, colsToUpdate...)
	delete(updates, "id")
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	assignments := make([]string, len(keys))
	for i, key := range keys {
		assignments[i] = key + " = EXCLUDED." + key
	}
	conflict := "ON CONFLICT (id) DO NOTHING"
	if len(assignments) > 0 {
		conflict = "ON CONFLICT (id) DO UPDATE SET " + strings.Join(assignments, ", ")
	}
	_, err = r.executeSQL(ctx, Insert(r.tableName).SetMap(values).Suffix(conflict))
	return id, err
}

func (r sqlRepository) delete(ctx context.Context, cond Sqlizer) error {
	_, err := r.executeSQL(ctx, Delete(r.tableName).Where(cond))
	return err
}

// deleteByID is for single-item deletes that must report a missing row; delete succeeds silently.
func (r sqlRepository) deleteByID(ctx context.Context, id string) error {
	count, err := r.executeSQL(ctx, Delete(r.tableName).Where(Eq{"id": id}))
	if err != nil {
		return err
	}
	if count == 0 {
		return model.ErrNotFound
	}
	return nil
}
