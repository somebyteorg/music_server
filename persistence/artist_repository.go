package persistence

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils"
	"github.com/navidrome/navidrome/utils/slice"
	"github.com/navidrome/navidrome/utils/str"
	"github.com/pocketbase/dbx"
)

type artistRepository struct {
	sqlRepository
	indexGroups utils.IndexGroups
}

type dbArtist struct {
	*model.Artist    `structs:",flatten"`
	SimilarArtists   string `structs:"-" json:"-"`
	LibraryStatsJSON string `structs:"-" json:"-"`
}

type dbSimilarArtist struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (a *dbArtist) PostScan() error {
	a.Artist.Stats = make(map[model.Role]model.ArtistStats)

	if a.LibraryStatsJSON != "" {
		var rawLibStats map[string]map[string]map[string]int64
		if err := json.Unmarshal([]byte(a.LibraryStatsJSON), &rawLibStats); err != nil {
			return fmt.Errorf("parsing artist stats from db: %w", err)
		}

		for _, stats := range rawLibStats {
			// Sum all libraries roles stats
			for key, stat := range stats {
				// Aggregate stats into the main Artist.Stats map
				artistStats := model.ArtistStats{
					SongCount:  int(stat["m"]),
					AlbumCount: int(stat["a"]),
					Size:       stat["s"],
				}

				// Store total stats into the main attributes
				if key == "total" {
					a.Artist.Size += artistStats.Size
					a.Artist.SongCount += artistStats.SongCount
					a.Artist.AlbumCount += artistStats.AlbumCount
				}

				role := model.RoleFromString(key)
				if role == model.RoleInvalid {
					continue
				}

				current := a.Artist.Stats[role]
				current.Size += artistStats.Size
				current.SongCount += artistStats.SongCount
				current.AlbumCount += artistStats.AlbumCount
				a.Artist.Stats[role] = current
			}
		}
	}

	a.Artist.SimilarArtists = nil
	if a.SimilarArtists == "" {
		return nil
	}
	var sa []dbSimilarArtist
	if err := json.Unmarshal([]byte(a.SimilarArtists), &sa); err != nil {
		return fmt.Errorf("parsing similar artists from db: %w", err)
	}
	for _, s := range sa {
		a.Artist.SimilarArtists = append(a.Artist.SimilarArtists, model.Artist{
			ID:   s.ID,
			Name: s.Name,
		})
	}
	return nil
}

func (a *dbArtist) PostMapArgs(m map[string]any) error {
	sa := make([]dbSimilarArtist, 0)
	for _, s := range a.Artist.SimilarArtists {
		sa = append(sa, dbSimilarArtist{ID: s.ID, Name: s.Name})
	}
	similarArtists, _ := json.Marshal(sa)
	m["similar_artists"] = string(similarArtists)
	// When adding a derived column here, also add it to the scanner's artist Put column list
	// in phase_1_folders.go, or rescans will never update it (how search_normalized went stale).
	m["full_text"] = formatFullText(a.Name, a.SortArtistName)
	m["search_normalized"] = str.NormalizeForFTS(a.Name)

	// Do not override the sort_artist_name and mbz_artist_id fields if they are empty
	// TODO: Better way to handle this?
	if v, ok := m["sort_artist_name"]; !ok || v.(string) == "" {
		delete(m, "sort_artist_name")
	}
	if v, ok := m["mbz_artist_id"]; !ok || v.(string) == "" {
		delete(m, "mbz_artist_id")
	}
	return nil
}

type dbArtists []dbArtist

func (dba dbArtists) toModels() model.Artists {
	res := make(model.Artists, len(dba))
	for i := range dba {
		res[i] = *dba[i].Artist
	}
	return res
}

func NewArtistRepository(db dbx.Builder) model.ArtistRepository {
	r := &artistRepository{}
	r.db = db
	r.indexGroups = utils.ParseIndexGroups(conf.Server.IndexGroups)
	r.tableName = "artist" // To be used by the idFilter below
	r.registerModel(&model.Artist{}, map[string]filterFunc{
		"id":         idFilter(r.tableName),
		"name":       fullTextFilter(r.tableName, "mbz_artist_id"),
		"starred":    annotationBoolFilter("starred"),
		"has_rating": annotationBoolFilter("rating"),
		"role":       roleFilter,
		"missing":    booleanFilter,
		"library_id": artistLibraryIdFilter,
	})
	r.setSortMappings(map[string]string{ //nolint:gosec
		"name":        "order_artist_name",
		"starred_at":  "starred, starred_at",
		"rated_at":    "rating, rated_at",
		"song_count":  "sum(coalesce((library_artist.stats #>> '{total,m}')::bigint, 0))",
		"album_count": "sum(coalesce((library_artist.stats #>> '{total,a}')::bigint, 0))",
		"size":        "sum(coalesce((library_artist.stats #>> '{total,s}')::bigint, 0))",

		// Stats by credits that are currently available
		"maincredit_song_count":  "sum(coalesce((library_artist.stats #>> '{maincredit,m}')::bigint, 0))",
		"maincredit_album_count": "sum(coalesce((library_artist.stats #>> '{maincredit,a}')::bigint, 0))",
		"maincredit_size":        "sum(coalesce((library_artist.stats #>> '{maincredit,s}')::bigint, 0))",
	})
	return r
}

func roleFilter(_ string, role any) Sqlizer {
	if role, ok := role.(string); ok {
		if safe, ok := sanitizeArtistStatsRole(role); ok && safe != "total" {
			return Expr("jsonb_extract_path_text(library_artist.stats, ?, 'm') IS NOT NULL", safe)
		}
	}
	return Eq{"1": 2}
}

// sanitizeArtistStatsRole allowlists values interpolated into JSON paths for artist
// stats (filter and sort). "total" is the aggregate key stored by the scanner.
// Unknown values must not reach SQL string concatenation.
func sanitizeArtistStatsRole(role string) (string, bool) {
	if role == "" || role == "total" {
		return "total", true
	}
	if _, ok := model.AllRoles[role]; ok {
		return role, true
	}
	return "", false
}

// artistLibraryIdFilter filters artists based on library access through the library_artist table
func artistLibraryIdFilter(_ string, value any) Sqlizer {
	return Eq{"library_artist.library_id": value}
}

// applyLibraryFilterToArtistQuery applies library filtering to artist queries through the library_artist junction table
func (r *artistRepository) applyLibraryFilterToArtistQuery(ctx context.Context, query SelectBuilder) SelectBuilder {
	user := loggedUser(ctx)
	// Join with library_artist first to ensure only artists with content in libraries are included
	// Exclude artists with empty stats (no actual content in the library)
	query = query.Join("library_artist on library_artist.artist_id = artist.id")
	//query = query.Join("library_artist on library_artist.artist_id = artist.id AND library_artist.stats != '{}'")

	if user.ID != invalidUserId && !user.HasAllLibraryAccess() {
		// Apply each user's selected libraries when browsing artists.
		query = query.Join("user_library on user_library.library_id = library_artist.library_id AND user_library.user_id = ?", user.ID)
	}

	return query
}

func (r *artistRepository) selectArtist(ctx context.Context, options ...model.QueryOptions) SelectBuilder {
	// Stats Format: {"1": {"albumartist": {"m": 10, "a": 5, "s": 1024}, "artist": {...}}, "2": {...}}
	query := r.newSelect(ctx, options...).Columns("artist.*",
		"jsonb_object_agg(library_artist.library_id, library_artist.stats) as library_stats_json")

	query = r.applyLibraryFilterToArtistQuery(ctx, query)
	query = query.GroupBy("artist.id")
	return r.withAnnotation(ctx, query, "artist.id")
}

func (r *artistRepository) CountAll(ctx context.Context, options ...model.QueryOptions) (int64, error) {
	query := r.newSelect(ctx)
	query = r.applyLibraryFilterToArtistQuery(ctx, query)
	// Only the annotation join is gated; the library_artist join above (and its count(distinct))
	// must stay, since an artist can span multiple libraries.
	if filtersNeedAnnotation(r.applyFilters(query, options...)) {
		query = r.withAnnotation(ctx, query, "artist.id")
	}
	return r.count(ctx, query, options...)
}

// Exists checks if an artist with the given ID exists in the database and is accessible by the current user.
func (r *artistRepository) Exists(ctx context.Context, id string) (bool, error) {
	// Create a query using the same library filtering logic as selectArtist()
	query := r.newSelect(ctx).Columns("count(distinct artist.id) as exist").Where(Eq{"artist.id": id})
	query = r.applyLibraryFilterToArtistQuery(ctx, query)

	var res struct{ Exist int64 }
	err := r.queryOne(ctx, query, &res)
	return res.Exist > 0, err
}

func (r *artistRepository) Put(ctx context.Context, a *model.Artist, colsToUpdate ...string) error {
	dba := &dbArtist{Artist: a}
	dba.CreatedAt = new(databaseNow())
	dba.UpdatedAt = dba.CreatedAt
	_, err := r.put(ctx, dba.ID, dba, colsToUpdate...)
	return err
}

func (r *artistRepository) UpdateExternalInfo(ctx context.Context, a *model.Artist) error {
	dba := &dbArtist{Artist: a}
	_, err := r.put(ctx, a.ID, dba,
		"biography", "small_image_url", "medium_image_url", "large_image_url",
		"similar_artists", "external_url", "external_info_updated_at")
	return err
}

func (r *artistRepository) Get(ctx context.Context, id string) (*model.Artist, error) {
	sel := r.selectArtist(ctx).Where(Eq{"artist.id": id})
	var dba dbArtists
	if err := r.queryAll(ctx, sel, &dba); err != nil {
		return nil, err
	}
	if len(dba) == 0 {
		return nil, model.ErrNotFound
	}
	res := dba.toModels()
	r.hydrateArtwork(ctx, res)
	return &res[0], nil
}

func (r *artistRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.Artists, error) {
	sel := r.selectArtist(ctx, options...)
	var dba dbArtists
	err := r.queryAll(ctx, sel, &dba)
	if err != nil {
		return nil, err
	}
	res := dba.toModels()
	r.hydrateArtwork(ctx, res)
	return res, err
}

// hydrateArtwork fills each artist's ImageHash/ImageAbsent from one batched item_artwork lookup.
func (r *artistRepository) hydrateArtwork(ctx context.Context, artists model.Artists) {
	hydrateItems(ctx, r.db, model.KindArtistArtwork, artists,
		func(a *model.Artist) (string, *model.ItemImage) { return a.ID, &a.ItemImage })
}

func (r *artistRepository) GetCursor(ctx context.Context, options ...model.QueryOptions) (model.ArtistCursor, error) {
	cursor, err := queryCursor[dbArtist](ctx, r.sqlRepository, r.selectArtist(ctx, options...))
	if err != nil {
		return nil, err
	}
	artists := wrapCursor(cursor, func(row dbArtist) *model.Artist { return row.Artist })
	return model.ArtistCursor(hydrateCursor(artists, func(batch []model.Artist) {
		r.hydrateArtwork(ctx, batch)
	})), nil
}

func (r *artistRepository) getIndexKey(a model.Artist) string {
	source := a.OrderArtistName
	if conf.Server.PreferSortTags {
		source = cmp.Or(a.SortArtistName, a.OrderArtistName)
	}
	name := strings.ToLower(source)
	for k, v := range r.indexGroups {
		if strings.HasPrefix(name, strings.ToLower(k)) {
			return v
		}
	}
	return "#"
}

// GetIndex returns a list of artists grouped by the first letter of their name, or by the index group if configured.
// It can filter by roles and libraries, and optionally include artists that are missing (i.e., have no albums).
// TODO Cache the index (recalculate at scan time)
func (r *artistRepository) GetIndex(ctx context.Context, includeMissing bool, libraryIds []int, roles ...model.Role) (model.ArtistIndexes, error) {
	// Validate library IDs. If no library IDs are provided, return an empty index.
	if len(libraryIds) == 0 {
		return nil, nil
	}

	options := model.QueryOptions{Sort: "name"}
	if len(roles) > 0 {
		roleFilters := slice.Map(roles, func(r model.Role) Sqlizer {
			return roleFilter("role", r.String())
		})
		options.Filters = Or(roleFilters)
	}
	if !includeMissing {
		if options.Filters == nil {
			options.Filters = Eq{"artist.missing": false}
		} else {
			options.Filters = And{options.Filters, Eq{"artist.missing": false}}
		}
	}

	libFilter := artistLibraryIdFilter("library_id", libraryIds)
	if options.Filters == nil {
		options.Filters = libFilter
	} else {
		options.Filters = And{options.Filters, libFilter}
	}

	artists, err := r.GetAll(ctx, options)
	if err != nil {
		return nil, err
	}

	var result model.ArtistIndexes
	for k, v := range slice.Group(artists, r.getIndexKey) {
		result = append(result, model.ArtistIndex{ID: k, Artists: v})
	}
	slices.SortFunc(result, func(a, b model.ArtistIndex) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return result, nil
}

func (r *artistRepository) purgeEmpty(ctx context.Context) error {
	orphanFilter := "id not in (select artist_id from album_artists)"

	// Collect uploaded image filenames before deleting
	sel := Select("uploaded_image").From(r.tableName).
		Where(orphanFilter).
		Where("uploaded_image != ''")
	var imageFiles []string
	if err := r.queryAllSlice(ctx, sel, &imageFiles); err != nil && !errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("collecting artist images for cleanup: %w", err)
	}

	// Delete orphan artists
	del := Delete(r.tableName).Where(orphanFilter)
	c, err := r.executeSQL(ctx, del)
	if err != nil {
		return fmt.Errorf("purging empty artists: %w", err)
	}
	if c > 0 {
		log.Debug(ctx, "Purged empty artists", "totalDeleted", c)
	}

	if len(imageFiles) == 0 {
		return nil
	}

	// Best-effort cleanup of uploaded image files
	log.Debug(ctx, "Cleaning up artist images", "totalImages", len(imageFiles))
	for _, filename := range imageFiles {
		path := model.UploadedImagePath(consts.EntityArtist, filename)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warn(ctx, "Failed to remove artist image during GC", "path", path, err)
		}
	}
	return nil
}

// markOrphansMissing flags as missing any non-missing artist with no library_artist row, keeping the
// search fast-path's `missing = false` filter correct (see searchCfg). Called wherever such a row can
// be dropped: RefreshStats cleanup and library deletion cascade.
func (r *artistRepository) markOrphansMissing(ctx context.Context) error {
	_, err := r.executeSQL(ctx, Expr(
		"update artist set missing = true where missing = false "+
			"and not exists (select 1 from library_artist where library_artist.artist_id = artist.id)"))
	if err != nil {
		return fmt.Errorf("marking orphaned artists missing: %w", err)
	}
	return nil
}

// markMissing marks artists as missing if all their albums are missing.
func (r *artistRepository) markMissing(ctx context.Context) error {
	q := Expr(`
with artists_with_non_missing_albums as (
    select distinct aa.artist_id
    from album_artists aa
    join album a on aa.album_id = a.id
    where a.missing = false
)
update artist
set missing = (artist.id not in (select artist_id from artists_with_non_missing_albums));
        `)
	_, err := r.executeSQL(ctx, q)
	if err != nil {
		return fmt.Errorf("marking missing artists: %w", err)
	}
	return nil
}

// RefreshPlayCounts updates the play count and last play date annotations for all artists, based
// on the media files associated with them.
func (r *artistRepository) RefreshPlayCounts(ctx context.Context) (int64, error) {
	query := Expr(`
with play_counts as (
    select user_id, jt.id as artist_id, sum(play_count) as total_play_count, max(play_date) as last_play_date
    from media_file
    join annotation on item_id = media_file.id AND item_type = 'media_file'
    CROSS JOIN LATERAL jsonb_to_recordset(coalesce(participants -> 'artist', '[]'::jsonb)) AS jt(id text)
    WHERE jt.id IS NOT NULL
    group by user_id, jt.id
)
insert into annotation (user_id, item_id, item_type, play_count, play_date)
select user_id, artist_id, 'artist', total_play_count, last_play_date
from play_counts
where total_play_count > 0
on conflict (user_id, item_id, item_type) do update
    set play_count = excluded.play_count,
        play_date  = excluded.play_date;
`)
	return r.executeSQL(ctx, query)
}

func (r *artistRepository) searchCfg() searchConfig {
	return searchConfig{NaturalOrder: "artist.id", OrderBy: []string{"sum(coalesce((library_artist.stats #>> '{total,m}')::bigint,0)) desc", "name"}, MBIDFields: []string{"mbz_artist_id"}}
}

func (r *artistRepository) Search(ctx context.Context, q string, options ...model.QueryOptions) (model.Artists, error) {
	var opts model.QueryOptions
	if len(options) > 0 {
		opts = options[0]
	}
	// Artists have no library_id column, so the library_id filter callers pass (same as albums/songs)
	// can't be applied directly: consume it as the search scope.
	scope := r.requestedLibraryIDs(opts.Filters)
	if isLibraryIDFilter(opts.Filters) {
		opts.Filters = nil
	}
	sq := r.selectArtist(ctx, opts)
	if scope != nil {
		// The user_library join already enforces current permissions; this predicate
		// additionally narrows results and statistics to the requested libraries.
		sq = sq.Where(artistLibraryIdFilter("library_id", scope))
	}
	var res dbArtists
	err := r.doSearch(ctx, sq, q, &res, r.searchCfg())
	if err != nil {
		return nil, fmt.Errorf("searching artist %q: %w", q, err)
	}
	artists := res.toModels()
	r.hydrateArtwork(ctx, artists)
	return artists, nil
}

// requestedLibraryIDs extracts the []int from an Eq{"library_id": ids} filter, or nil if filter is
// not that shape.
func (r *artistRepository) requestedLibraryIDs(filter Sqlizer) []int {
	eq, ok := filter.(Eq)
	if !ok {
		return nil
	}
	ids, _ := eq["library_id"].([]int)
	return ids
}

// isLibraryIDFilter reports whether the filter is an Eq carrying a library_id key, so Search can
// consume it before it reaches the bare artist table (which has no library_id column).
func isLibraryIDFilter(filter Sqlizer) bool {
	eq, ok := filter.(Eq)
	if !ok {
		return false
	}
	_, ok = eq["library_id"]
	return ok
}

func (r *artistRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *artistRepository) Read(ctx context.Context, id string) (*model.Artist, error) {
	return r.Get(ctx, id)
}

// sortMappingsForRole copies the shared mappings so a role-specific sort never leaks into them.
// Anything sanitizeArtistStatsRole rejects falls back to the "total" aggregate.
func (r *artistRepository) sortMappingsForRole(options ...rest.QueryOptions) map[string]string {
	role := "total"
	if len(options) > 0 {
		if v, ok := options[0].Filters["role"].(string); ok {
			if safe, ok := sanitizeArtistStatsRole(v); ok {
				role = safe
			}
		}
	}
	mappings := maps.Clone(r.sortMappings)
	mappings["song_count"] = "sum(coalesce((library_artist.stats #>> '{" + role + ",m}')::bigint, 0))"
	mappings["album_count"] = "sum(coalesce((library_artist.stats #>> '{" + role + ",a}')::bigint, 0))"
	mappings["size"] = "sum(coalesce((library_artist.stats #>> '{" + role + ",s}')::bigint, 0))"
	return mappings
}

func (r *artistRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.Artist, error) {
	scoped := *r
	scoped.sortMappings = r.sortMappingsForRole(options...)
	return scoped.GetAll(ctx, scoped.parseRestOptions(ctx, options...))
}

var _ model.ArtistRepository = (*artistRepository)(nil)
var _ rest.Repository[model.Artist] = (*artistRepository)(nil)
