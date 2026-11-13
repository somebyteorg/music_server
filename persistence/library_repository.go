package persistence

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type libraryRepository struct {
	sqlRepository
}

func NewLibraryRepository(db dbx.Builder) model.LibraryRepository {
	r := &libraryRepository{}
	r.db = db
	r.registerModel(&model.Library{}, nil)
	return r
}

func (r *libraryRepository) Get(ctx context.Context, id int) (*model.Library, error) {
	sq := r.newSelect(ctx).Columns("*").Where(Eq{"id": id})
	var res model.Library
	err := r.queryOne(ctx, sq, &res)
	return &res, err
}

func (r *libraryRepository) GetPath(ctx context.Context, id int) (string, error) {
	var result struct{ Path string }
	err := r.queryOne(ctx, Select("path").From(r.tableName).Where(Eq{"id": id}), &result)
	return result.Path, err
}

func (r *libraryRepository) Put(ctx context.Context, l *model.Library, colsToUpdate ...string) error {
	return r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		return scoped.putLibrary(ctx, l, colsToUpdate...)
	})
}

func (r *libraryRepository) putLibrary(ctx context.Context, l *model.Library, colsToUpdate ...string) error {
	if l.ID == model.DefaultLibraryID {
		currentLib, err := r.Get(ctx, 1)
		// if we are creating it, it's ok.
		if err == nil { // it exists, so we are updating it
			if currentLib.Path != l.Path {
				return fmt.Errorf("%w: path for library with ID 1 cannot be changed", model.ErrValidation)
			}
		}
	}

	var err error
	l.UpdatedAt = databaseNow()
	if l.ID == 0 {
		// Insert with autoassigned ID
		l.CreatedAt = databaseNow()
		err = r.db.Model(l).WithContext(ctx).Insert()
	} else {
		// Try to update first
		cols := selectUpdateColumns(map[string]any{
			"name":              l.Name,
			"path":              l.Path,
			"remote_path":       l.RemotePath,
			"default_new_users": l.DefaultNewUsers,
			"pid_album":         l.PIDAlbum,
			"pid_track":         l.PIDTrack,
		}, colsToUpdate...)
		cols["updated_at"] = l.UpdatedAt
		sq := Update(r.tableName).SetMap(cols).Where(Eq{"id": l.ID})
		rowsAffected, updateErr := r.executeSQL(ctx, sq)
		if updateErr != nil {
			return updateErr
		}

		// If no rows were affected, the record doesn't exist, so insert it
		if rowsAffected == 0 {
			l.CreatedAt = databaseNow()
			l.UpdatedAt = databaseNow()
			err = r.db.Model(l).WithContext(ctx).Insert()
		}
	}
	if err != nil {
		return err
	}

	// Auto-assign all libraries to all admin users
	sql := Expr(`
INSERT INTO user_library (user_id, library_id)
SELECT u.id, l.id
FROM app_user u
CROSS JOIN library l
WHERE u.is_admin = true
ON CONFLICT (user_id, library_id) DO NOTHING;`,
	)
	if _, err = r.executeSQL(ctx, sql); err != nil {
		return fmt.Errorf("failed to assign library to admin users: %w", err)
	}

	return nil
}

// TODO Remove this method when we have a proper UI to add libraries
// This is a temporary method to store the music folder path from the config in the DB
func (r *libraryRepository) StoreMusicFolder(ctx context.Context) error {
	sq := Update(r.tableName).Set("path", conf.Server.MusicFolder).
		Set("updated_at", databaseNow()).
		Where(Eq{"id": model.DefaultLibraryID}).
		Where(NotEq{"path": conf.Server.MusicFolder})
	_, err := r.executeSQL(ctx, sq)
	return err
}

func (r *libraryRepository) AddArtist(ctx context.Context, id int, artistID string) error {
	sq := Insert("library_artist").Columns("library_id", "artist_id").Values(id, artistID).
		Suffix(`on conflict(library_id, artist_id) do nothing`)
	_, err := r.executeSQL(ctx, sq)
	if err != nil {
		return err
	}
	return nil
}

func (r *libraryRepository) Delete(ctx context.Context, id int) error {
	return r.withTx(ctx, func(base sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = base
		return scoped.deleteLibrary(ctx, id)
	})
}

func (r *libraryRepository) deleteLibrary(ctx context.Context, id int) error {
	if !loggedUser(ctx).IsAdmin {
		return model.ErrNotAuthorized
	}
	if id == 1 {
		return fmt.Errorf("%w: library with ID 1 cannot be deleted", model.ErrValidation)
	}

	err := r.delete(ctx, Eq{"id": id})
	if err != nil {
		return err
	}

	// The cascade above can drop an artist's last library_artist row; reconcile any such orphans.
	if err := NewArtistRepository(r.db).(*artistRepository).markOrphansMissing(ctx); err != nil {
		return fmt.Errorf("marking orphaned artists missing after deleting library %d: %w", id, err)
	}

	// Clean up orphaned plugin references for the deleted library
	if err := cleanupPluginLibraryReferences(ctx, r.db, id); err != nil {
		return fmt.Errorf("cleaning plugin library references: %w", err)
	}
	return nil
}

func (r *libraryRepository) GetAll(ctx context.Context, ops ...model.QueryOptions) (model.Libraries, error) {
	sq := r.newSelect(ctx, ops...).Columns("*")
	res := model.Libraries{}
	err := r.queryAll(ctx, sq, &res)
	return res, err
}

func (r *libraryRepository) CountAll(ctx context.Context, ops ...model.QueryOptions) (int64, error) {
	sq := r.newSelect(ctx, ops...)
	return r.count(ctx, sq)
}

// User-library association methods

func (r *libraryRepository) GetUsersWithLibraryAccess(ctx context.Context, libraryID int) (model.Users, error) {
	sel := Select("u.*").
		From("app_user u").
		Join("user_library ul ON u.id = ul.user_id").
		Where(Eq{"ul.library_id": libraryID}).
		OrderBy("u.name")

	var res model.Users
	err := r.queryAll(ctx, sel, &res)
	return res, err
}

// REST interface methods

func (r *libraryRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *libraryRepository) Read(ctx context.Context, id string) (*model.Library, error) {
	idInt, err := strconv.Atoi(id)
	if err != nil {
		log.Trace(ctx, "invalid library id: %s", id, err)
		return nil, rest.ErrNotFound
	}
	return r.Get(ctx, idInt)
}

func (r *libraryRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.Library, error) {
	return r.GetAll(ctx, r.parseRestOptions(ctx, options...))
}

var _ model.LibraryRepository = (*libraryRepository)(nil)
var _ rest.Repository[model.Library] = (*libraryRepository)(nil)
