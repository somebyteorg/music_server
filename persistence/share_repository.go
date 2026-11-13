package persistence

import (
	"context"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type shareRepository struct {
	sqlRepository
}

func NewShareRepository(db dbx.Builder) model.ShareRepository {
	r := &shareRepository{}
	r.db = db
	r.registerModel(&model.Share{}, nil)
	r.setSortMappings(map[string]string{
		"username": "username",
	})
	return r
}

func (r *shareRepository) Delete(ctx context.Context, ids ...string) error {
	return r.deleteOwnedAll(ctx, ids...)
}

func (r *shareRepository) selectShare(ctx context.Context, options ...model.QueryOptions) SelectBuilder {
	return r.newSelect(ctx, options...).Join("app_user u on u.id = share.user_id").
		Columns("share.*", "user_name as username").
		Where(r.addRestriction(ctx))
}

func (r *shareRepository) Exists(ctx context.Context, id string) (bool, error) {
	return r.exists(ctx, r.addRestriction(ctx, And{Eq{"id": id}}))
}

func (r *shareRepository) Get(ctx context.Context, id string) (*model.Share, error) {
	sel := r.selectShare(ctx).Where(Eq{"share.id": id})
	var res model.Share
	err := r.queryOne(ctx, sel, &res)
	if err != nil {
		return nil, err
	}
	shares := model.Shares{res}
	err = r.hydrateShares(ctx, shares)
	return &shares[0], err
}

func (r *shareRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.Shares, error) {
	sq := r.selectShare(ctx, options...)
	res := model.Shares{}
	err := r.queryAll(ctx, sq, &res)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateShares(ctx, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (r *shareRepository) Update(ctx context.Context, id string, entity model.Share, cols ...string) error {
	s := &entity
	s.ID = id
	s.UpdatedAt = databaseNow()
	if len(cols) > 0 {
		cols = append(cols, "updated_at")
	}
	return r.updateOwned(ctx, id, s, cols...)
}

func (r *shareRepository) Save(ctx context.Context, s *model.Share) (string, error) {
	// TODO Validate record
	// Owner is server-managed: for an authenticated request, never trust a
	// client-supplied UserID, as it drives the share's library-access context.
	u := loggedUser(ctx)
	if u.ID != invalidUserId || s.UserID == "" {
		s.UserID = u.ID
	}
	s.CreatedAt = databaseNow()
	s.UpdatedAt = databaseNow()
	return r.put(ctx, s.ID, s)
}

func (r *shareRepository) CountAll(ctx context.Context, options ...model.QueryOptions) (int64, error) {
	return r.count(ctx, r.selectShare(ctx), options...)
}

func (r *shareRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *shareRepository) Read(ctx context.Context, id string) (*model.Share, error) {
	sel := r.selectShare(ctx).Where(Eq{"share.id": id})
	var res model.Share
	err := r.queryOne(ctx, sel, &res)
	return &res, err
}

func (r *shareRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.Share, error) {
	sq := r.selectShare(ctx, r.parseRestOptions(ctx, options...))
	res := model.Shares{}
	err := r.queryAll(ctx, sq, &res)
	return res, err
}

var _ model.ShareRepository = (*shareRepository)(nil)
var _ rest.Repository[model.Share] = (*shareRepository)(nil)
var _ rest.Persistable[model.Share] = (*shareRepository)(nil)
