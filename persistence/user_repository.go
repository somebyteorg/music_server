package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/criteria"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils/slice"
	"github.com/pocketbase/dbx"
)

type userRepository struct {
	sqlRepository
}

type dbUser struct {
	*model.User   `structs:",flatten"`
	LibrariesJSON string `structs:"-" json:"-"`
}

func (u *dbUser) PostScan() error {
	if u.LibrariesJSON != "" {
		if err := json.Unmarshal([]byte(u.LibrariesJSON), &u.User.Libraries); err != nil {
			return fmt.Errorf("parsing user libraries from db: %w", err)
		}
	}
	return nil
}

type dbUsers []dbUser

func (us dbUsers) toModels() model.Users {
	return slice.Map(us, func(u dbUser) model.User { return *u.User })
}

func NewUserRepository(db dbx.Builder) model.UserRepository {
	r := &userRepository{}
	r.db = db
	r.tableName = "app_user"
	r.registerModel(&model.User{}, map[string]filterFunc{
		"id":       idFilter(r.tableName),
		"password": invalidFilter,
		"name":     startsWithFilter(r.tableName + ".name"),
	})
	return r
}

// selectUserWithLibraries returns a SelectBuilder that includes library information
func (r *userRepository) selectUserWithLibraries(ctx context.Context, options ...model.QueryOptions) SelectBuilder {
	return r.newSelect(ctx, options...).
		Columns(`app_user.id`, `app_user.user_name`, `app_user.is_can_down`,
			`app_user.name`, `app_user.email`, `app_user.is_admin`,
			`COALESCE(app_user.password, '') AS password`, `app_user.token_epoch`,
			`app_user.last_login_at`, `app_user.last_access_at`, `app_user.created_at`, `app_user.updated_at`,
			`app_user.scrobble_filter`,
			`COALESCE(jsonb_agg(jsonb_build_object(
				'id', library.id,
				'name', library.name,
				'path', library.path,
				'remote_path', library.remote_path,
				'last_scan_at', library.last_scan_at,
				'last_scan_started_at', library.last_scan_started_at,
				'full_scan_in_progress', library.full_scan_in_progress,
				'updated_at', library.updated_at,
				'created_at', library.created_at
			)) FILTER (WHERE library.id IS NOT NULL), '[]') AS libraries_json`).
		LeftJoin("user_library ul ON app_user.id = ul.user_id").
		LeftJoin("library ON ul.library_id = library.id").
		GroupBy("app_user.id")
}

func (r *userRepository) CountAll(ctx context.Context, qo ...model.QueryOptions) (int64, error) {
	return r.count(ctx, Select(), qo...)
}

func (r *userRepository) Get(ctx context.Context, id string) (*model.User, error) {
	sel := r.selectUserWithLibraries(ctx).Where(Eq{"app_user.id": id})
	var res dbUser
	err := r.queryOne(ctx, sel, &res)
	if err != nil {
		return nil, err
	}
	return res.User, nil
}

func (r *userRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.Users, error) {
	sel := r.selectUserWithLibraries(ctx, options...)
	var res dbUsers
	err := r.queryAll(ctx, sel, &res)
	if err != nil {
		return nil, err
	}
	return res.toModels(), nil
}

func (r *userRepository) Put(ctx context.Context, u *model.User) error {
	return r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		return scoped.putUser(ctx, u)
	})
}

func (r *userRepository) putUser(ctx context.Context, u *model.User) error {
	if u.ID == "" {
		u.ID = id.NewRandom()
	}
	u.UpdatedAt = databaseNow()
	values, err := toSQLArgs(*u)
	if err != nil {
		return fmt.Errorf("error converting user to SQL args: %w", err)
	}
	delete(values, "current_password")

	// The epoch bump rides the password UPDATE: as two statements they can interleave with a
	// concurrent change and leave a session valid that the other change should have revoked.
	update := Update(r.tableName).Where(Eq{"id": u.ID}).SetMap(values)
	var isNewUser bool
	var epoch int
	if u.NewPassword != "" {
		var res struct{ TokenEpoch int }
		err = r.queryOne(ctx, update.Set("token_epoch", Expr("token_epoch + 1")).
			Suffix("RETURNING token_epoch"), &res)
		switch {
		case errors.Is(err, model.ErrNotFound):
			isNewUser = true
		case err != nil:
			return err
		default:
			epoch = res.TokenEpoch
		}
	} else {
		count, err := r.executeSQL(ctx, update)
		if err != nil {
			return err
		}
		isNewUser = count == 0
	}
	if isNewUser {
		values["created_at"] = databaseNow()
		insert := Insert(r.tableName).SetMap(values)
		_, err = r.executeSQL(ctx, insert)
		if err != nil {
			return err
		}
	}

	// Initialize the selection once; later profile updates preserve it for every role.
	if isNewUser {
		sql := Expr(
			"INSERT INTO user_library (user_id, library_id) SELECT ?, id FROM library WHERE default_new_users = true ON CONFLICT DO NOTHING",
			u.ID,
		)
		if _, err := r.executeSQL(ctx, sql); err != nil {
			return fmt.Errorf("failed to assign default libraries to new user: %w", err)
		}
	}

	// Only the caller's own token can be refreshed in-flight; an admin resetting another
	// user must keep their own epoch.
	if u.NewPassword != "" && !isNewUser && loggedUser(ctx).ID == u.ID {
		request.SetTokenEpoch(ctx, epoch)
	}

	return nil
}

func (r *userRepository) FindFirstAdmin(ctx context.Context) (*model.User, error) {
	sel := r.selectUserWithLibraries(ctx, model.QueryOptions{Sort: "updated_at", Max: 1}).Where(Eq{"app_user.is_admin": true})
	var usr dbUser
	err := r.queryOne(ctx, sel, &usr)
	if err != nil {
		return nil, err
	}
	return usr.User, nil
}

func (r *userRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	sel := r.selectUserWithLibraries(ctx).Where(Expr("lower(app_user.user_name) = lower(?)", username))
	var usr dbUser
	err := r.queryOne(ctx, sel, &usr)
	if err != nil {
		return nil, err
	}
	return usr.User, nil
}

func (r *userRepository) FindByUsernameWithPassword(ctx context.Context, username string) (*model.User, error) {
	return r.FindByUsername(ctx, username)
}

func (r *userRepository) SyncEmosUser(ctx context.Context, emosID, username string, canDownload *bool, password *string) (*model.User, error) {
	var user *model.User
	err := r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		// Shared with music_api so simultaneous first logins create one account.
		if _, err := scoped.executeSQL(ctx, Expr("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "emos-user:"+emosID)); err != nil {
			return err
		}
		var result dbUser
		err := scoped.queryOne(ctx, scoped.selectUserWithLibraries(ctx).
			Where(Eq{"app_user.id": emosID}), &result)
		isNewUser := errors.Is(err, model.ErrNotFound)
		if isNewUser {
			user = &model.User{ID: emosID, UserName: username, Name: username}
		} else if err != nil {
			return err
		} else {
			user = result.User
		}
		// Quick login reuses existing account details; password login refreshes the EMOS profile.
		if canDownload != nil {
			user.UserName = username
			user.CanDownload = *canDownload
		}
		if isNewUser {
			if password != nil {
				user.NewPassword = *password
			}
			err = scoped.putUser(ctx, user)
		} else {
			// Login only refreshes EMOS profile fields, preserving locally managed admin rights.
			update := Update(scoped.tableName).Where(Eq{"id": user.ID}).Set("updated_at", databaseNow())
			if canDownload != nil {
				update = update.Set("user_name", username).Set("is_can_down", *canDownload)
			}
			if password != nil {
				update = update.Set("password", *password)
			}
			_, err = scoped.executeSQL(ctx, update)
		}
		if err != nil {
			return err
		}
		if err := scoped.UpdateLastLoginAt(ctx, user.ID); err != nil {
			return err
		}
		user, err = scoped.Get(ctx, user.ID)
		return err
	})
	return user, err
}

func (r *userRepository) UpdateLastLoginAt(ctx context.Context, id string) error {
	upd := Update(r.tableName).Where(Eq{"id": id}).Set("last_login_at", databaseNow())
	_, err := r.executeSQL(ctx, upd)
	return err
}

func (r *userRepository) UpdateLastAccessAt(ctx context.Context, id string) error {
	now := databaseNow()
	upd := Update(r.tableName).Where(Eq{"id": id}).Set("last_access_at", now)
	_, err := r.executeSQL(ctx, upd)
	return err
}

func (r *userRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	usr := loggedUser(ctx)
	if !usr.IsAdmin {
		return 0, rest.ErrPermissionDenied
	}
	return r.CountAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *userRepository) Read(ctx context.Context, id string) (*model.User, error) {
	usr := loggedUser(ctx)
	if !usr.IsAdmin && usr.ID != id {
		return nil, rest.ErrPermissionDenied
	}
	return r.Get(ctx, id)
}

func (r *userRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.User, error) {
	usr := loggedUser(ctx)
	if !usr.IsAdmin {
		return nil, rest.ErrPermissionDenied
	}
	return r.GetAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *userRepository) Save(ctx context.Context, u *model.User) (string, error) {
	return "", rest.ErrPermissionDenied
}

func (r *userRepository) Update(ctx context.Context, id string, entity model.User, _ ...string) error {
	u := &entity
	u.ID = id
	usr := loggedUser(ctx)
	if !usr.IsAdmin && usr.ID != u.ID {
		return rest.ErrPermissionDenied
	}
	if !usr.IsAdmin {
		if !conf.Server.EnableUserEditing {
			return rest.ErrPermissionDenied
		}
		u.IsAdmin = false
		u.UserName = usr.UserName
	}

	if u.NewPassword != "" || u.CurrentPassword != "" {
		return rest.ErrPermissionDenied
	}
	existing, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	u.CanDownload = existing.CanDownload
	u.UserName = existing.UserName
	u.IsAdmin = false
	if err := validateUsernameUnique(ctx, r, u); err != nil {
		return err
	}
	if err := validateScrobbleFilter(u); err != nil {
		return err
	}
	return r.Put(ctx, u)
}

func validatePasswordChange(newUser *model.User, logged *model.User) error {
	err := &rest.ValidationError{Errors: map[string]string{}}
	if logged.IsAdmin && newUser.ID != logged.ID {
		return nil
	}
	if newUser.NewPassword == "" {
		if newUser.CurrentPassword == "" {
			return nil
		}
		err.Errors["password"] = "ra.validation.required"
	}

	if !strings.HasPrefix(logged.Password, consts.PasswordAutogenPrefix) {
		if newUser.CurrentPassword == "" {
			err.Errors["currentPassword"] = "ra.validation.required"
		}
		if newUser.CurrentPassword != logged.Password {
			err.Errors["currentPassword"] = "ra.validation.passwordDoesNotMatch"
		}
	}
	if len(err.Errors) > 0 {
		return err
	}
	return nil
}

func validateUsernameUnique(ctx context.Context, r model.UserRepository, u *model.User) error {
	usr, err := r.FindByUsername(ctx, u.UserName)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if usr.ID != u.ID {
		return &rest.ValidationError{Errors: map[string]string{"userName": "ra.validation.unique"}}
	}
	return nil
}

func validateScrobbleFilter(u *model.User) error {
	u.ScrobbleFilter = strings.TrimSpace(u.ScrobbleFilter)
	if u.ScrobbleFilter == "" {
		return nil
	}
	var c criteria.Criteria
	if err := json.Unmarshal([]byte(u.ScrobbleFilter), &c); err != nil {
		return invalidScrobbleFilter()
	}
	// A filter is a per-track test, so a result-set size means nothing here. Reject it
	// rather than silently ignoring part of a rule copied from a smart playlist.
	if c.Limit > 0 || c.LimitPercent > 0 || c.Offset > 0 || c.RefreshDelay > 0 {
		return invalidScrobbleFilter()
	}
	// Building the WHERE clause is what validates field names and operators
	if _, err := newSmartPlaylistCriteria(c).where(); err != nil {
		return invalidScrobbleFilter()
	}
	return nil
}

func invalidScrobbleFilter() error {
	return &rest.ValidationError{Errors: map[string]string{
		"scrobbleFilter": "resources.user.validation.invalidScrobbleFilter",
	}}
}

func (r *userRepository) Delete(ctx context.Context, ids ...string) error {
	return r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		return scoped.deleteUsers(ctx, ids...)
	})
}

func (r *userRepository) deleteUsers(ctx context.Context, ids ...string) error {
	usr := loggedUser(ctx)
	if !usr.IsAdmin {
		return rest.ErrPermissionDenied
	}
	for _, id := range ids {
		if err := r.deleteByID(ctx, id); err != nil {
			return err
		}
		if err := cleanupPluginUserReferences(ctx, r.db, id); err != nil {
			return fmt.Errorf("cleaning plugin user references: %w", err)
		}
	}
	return nil
}

// Library association methods

func (r *userRepository) GetUserLibraries(ctx context.Context, userID string) (model.Libraries, error) {
	sel := Select("l.*").
		From("library l").
		Join("user_library ul ON l.id = ul.library_id").
		Where(Eq{"ul.user_id": userID}).
		OrderBy("l.name")

	var res model.Libraries
	err := r.queryAll(ctx, sel, &res)
	return res, err
}

func (r *userRepository) SetUserLibraries(ctx context.Context, userID string, libraryIDs []int) error {
	return r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		return scoped.setUserLibraries(ctx, userID, libraryIDs)
	})
}

func (r *userRepository) setUserLibraries(ctx context.Context, userID string, libraryIDs []int) error {
	// Remove existing associations
	delSql := Delete("user_library").Where(Eq{"user_id": userID})
	if _, err := r.executeSQL(ctx, delSql); err != nil {
		return err
	}

	// Add new associations
	if len(libraryIDs) > 0 {
		insert := Insert("user_library").Columns("user_id", "library_id")
		for _, libID := range libraryIDs {
			insert = insert.Values(userID, libID)
		}
		_, err := r.executeSQL(ctx, insert)
		return err
	}
	return nil
}

var _ model.UserRepository = (*userRepository)(nil)
var _ rest.Repository[model.User] = (*userRepository)(nil)
var _ rest.Persistable[model.User] = (*userRepository)(nil)
