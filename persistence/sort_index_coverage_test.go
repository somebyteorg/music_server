package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Sort mappings must retain an index path on large libraries. Disable sequential
// scans within the EXPLAIN transaction so tiny fixtures do not favor a full sort.
// Incremental sorts are allowed: only groups sharing the index prefix are sorted.
var _ = Describe("Sort index coverage", func() {
	conn := db.Db()

	type repoCase struct {
		table   string
		newRepo func(ctx context.Context) *sqlRepository
		// sort mapping -> reason it cannot be served by an index
		exceptions map[string]string
	}

	cases := []repoCase{
		{
			table: "media_file",
			newRepo: func(ctx context.Context) *sqlRepository {
				return &NewMediaFileRepository(GetDBXBuilder()).(*mediaFileRepository).sqlRepository
			},
			exceptions: map[string]string{
				"random":     "not a column sort",
				"starred_at": "sorts on annotation join columns",
				"rated_at":   "sorts on annotation join columns",
				"play_count": "sorts on annotation join columns",
				"play_date":  "sorts on annotation join columns",
				"rating":     "sorts on annotation join columns",
				"comment":    "UI-sortable but rarely used; not worth an index",
			},
		},
		{
			table: "album",
			newRepo: func(ctx context.Context) *sqlRepository {
				return &NewAlbumRepository(GetDBXBuilder()).(*albumRepository).sqlRepository
			},
			exceptions: map[string]string{
				"random":     "not a column sort",
				"starred_at": "sorts on annotation join columns",
				"rated_at":   "sorts on annotation join columns",
				"max_year":   "coalesce expression over original_date/max_year, no expression index",
			},
		},
		{
			table: "artist",
			newRepo: func(ctx context.Context) *sqlRepository {
				return &NewArtistRepository(GetDBXBuilder()).(*artistRepository).sqlRepository
			},
			exceptions: map[string]string{ //nolint:gosec // G101 false positive, same as the artist sortMappings
				"starred_at":             "sorts on annotation join columns",
				"rated_at":               "sorts on annotation join columns",
				"song_count":             "JSON expression over stats column",
				"album_count":            "JSON expression over stats column",
				"size":                   "JSON expression over stats column",
				"maincredit_song_count":  "aggregate over JSON stats",
				"maincredit_album_count": "aggregate over JSON stats",
				"maincredit_size":        "aggregate over JSON stats",
			},
		},
	}

	newCtx := func() context.Context {
		ctx := log.NewContext(GinkgoT().Context())
		return request.WithUser(ctx, model.User{ID: "userid"})
	}

	for _, c := range cases {
		It(fmt.Sprintf("uses an index for every sort mapping on %s", c.table), func() {
			r := c.newRepo(newCtx())
			for _, sort := range slices.Sorted(maps.Keys(r.sortMappings)) {
				if _, ok := c.exceptions[sort]; ok {
					continue
				}
				for _, dir := range []string{"asc", "desc"} {
					orderBy := r.buildSortOrder(sort, dir)
					Expect(checkSortUsesIndex(conn, c.table, orderBy)).To(Succeed(),
						"sort %q (%s) on table %q needs an index. Create one matching its ORDER BY, or, if it cannot be served by an index, add it to exceptions with the reason",
						sort, dir, c.table)
				}
			}
		})

		It(fmt.Sprintf("has no stale exceptions entries for %s", c.table), func() {
			r := c.newRepo(newCtx())
			for _, sort := range slices.Sorted(maps.Keys(c.exceptions)) {
				Expect(r.sortMappings).To(HaveKey(sort),
					"exceptions entry %q on table %q does not match any sort mapping - remove it", sort, c.table)
			}
		})
	}

	It("uses an index for recently_added when RecentlyAddedByModTime is enabled", func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.RecentlyAddedByModTime = true
		for _, c := range cases[:2] { // media_file and album
			r := c.newRepo(newCtx())
			for _, dir := range []string{"asc", "desc"} {
				orderBy := r.buildSortOrder("recently_added", dir)
				Expect(checkSortUsesIndex(conn, c.table, orderBy)).To(Succeed(),
					"sort recently_added (%s) on table %q", dir, c.table)
			}
		}
	})
})

func checkSortUsesIndex(conn *sql.DB, table, orderBy string) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("SET LOCAL enable_seqscan = off"); err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf("EXPLAIN (FORMAT JSON) SELECT * FROM %s ORDER BY %s LIMIT 15", table, orderBy)).Scan(&raw); err != nil {
		return fmt.Errorf("explain failed for ORDER BY %q: %w", orderBy, err)
	}
	var plans []struct{ Plan queryPlan }
	if err := json.Unmarshal(raw, &plans); err != nil {
		return err
	}
	var fullSort func(queryPlan) bool
	fullSort = func(plan queryPlan) bool {
		return plan.NodeType == "Sort" || slices.ContainsFunc(plan.Plans, fullSort)
	}
	if len(plans) != 1 || fullSort(plans[0].Plan) {
		return fmt.Errorf("no index satisfies ORDER BY %s: %s", orderBy, raw)
	}
	return nil
}

type queryPlan struct {
	NodeType string      `json:"Node Type"`
	Plans    []queryPlan `json:"Plans"`
}
