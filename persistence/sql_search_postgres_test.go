package persistence

import (
	"context"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL Integration Search", func() {
	var (
		mr  model.MediaFileRepository
		alr model.AlbumRepository
		arr model.ArtistRepository
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = request.WithUser(log.NewContext(GinkgoT().Context()), adminUser)
		conn := GetDBXBuilder()
		mr = NewMediaFileRepository(conn)
		alr = NewAlbumRepository(conn)
		arr = NewArtistRepository(conn)
	})

	Describe("MediaFile search", func() {
		It("finds media files by title", func() {
			results, err := mr.Search(ctx, "Radioactivity", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Title).To(Equal("Radioactivity"))
			Expect(results[0].ID).To(Equal(songRadioactivity.ID))
		})

		It("finds media files by artist name", func() {
			results, err := mr.Search(ctx, "Beatles", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			for _, r := range results {
				Expect(r.Artist).To(Equal("The Beatles"))
			}
		})
	})

	Describe("Album search", func() {
		It("finds albums by name", func() {
			results, err := alr.Search(ctx, "Sgt Peppers", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("Sgt Peppers"))
			Expect(results[0].ID).To(Equal(albumSgtPeppers.ID))
		})

		It("finds albums with multi-word search", func() {
			results, err := alr.Search(ctx, "Abbey Road", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))
		})
	})

	Describe("Artist search", func() {
		It("finds artists by name", func() {
			results, err := arr.Search(ctx, "Kraftwerk", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("Kraftwerk"))
			Expect(results[0].ID).To(Equal(artistKraftwerk.ID))
		})
	})

	Describe("CJK search", func() {
		It("finds media files by CJK title", func() {
			results, err := mr.Search(ctx, "プラチナ", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Title).To(Equal("プラチナ・ジェット"))
			Expect(results[0].ID).To(Equal(songCJK.ID))
		})

		It("finds media files by CJK artist name", func() {
			results, err := mr.Search(ctx, "シートベルツ", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Artist).To(Equal("シートベルツ"))
		})

		It("finds albums by CJK artist name", func() {
			results, err := alr.Search(ctx, "シートベルツ", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("COWBOY BEBOP"))
			Expect(results[0].ID).To(Equal(albumCJK.ID))
		})

		It("finds artists by CJK name", func() {
			results, err := arr.Search(ctx, "シートベルツ", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("シートベルツ"))
			Expect(results[0].ID).To(Equal(artistCJK.ID))
		})
	})

	Describe("Album version search", func() {
		It("finds albums by version tag via FTS", func() {
			results, err := alr.Search(ctx, "Deluxe", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal(albumWithVersion.ID))
		})
	})

	Describe("Punctuation-only search", func() {
		It("finds media files with punctuation-only title", func() {
			results, err := mr.Search(ctx, "!!!!!!!", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Title).To(Equal("!!!!!!!"))
			Expect(results[0].ID).To(Equal(songPunctuation.ID))
		})
	})

	Describe("Single-character search (doSearch min-length guard)", func() {
		It("returns empty results for single-char query via Search", func() {
			results, err := mr.Search(ctx, "a", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(BeEmpty(), "doSearch should reject single-char queries")
		})
	})

	Describe("Max=0 means no limit (regression: must not produce LIMIT 0)", func() {
		It("returns results with Max=0", func() {
			results, err := mr.Search(ctx, "Beatles", model.QueryOptions{Max: 0})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).ToNot(BeEmpty(), "Max=0 should mean no limit, not LIMIT 0")
		})
	})

	Describe("Exact-match ranking", func() {
		BeforeEach(func() {
			// Registered before the inserts so a mid-loop failure cannot leak corpus rows.
			DeferCleanup(func() {
				// library_artist rows are removed by the artist_id ON DELETE CASCADE FK.
				_, err := GetDBXBuilder().NewQuery("DELETE FROM artist WHERE id LIKE 'fts-rank-%'").Execute()
				Expect(err).ToNot(HaveOccurred())
			})
			// Corpus has no competing exact-word names ("Mo X"): exact-vs-exact order depends
			// on corpus statistics; the guaranteed property is exact > prefix.
			for _, a := range []model.Artist{
				{ID: "fts-rank-1", Name: "MØ", OrderArtistName: "mø"},
				{ID: "fts-rank-2", Name: "Modest Mouse", OrderArtistName: "modest mouse"},
				{ID: "fts-rank-3", Name: "Morrissey", OrderArtistName: "morrissey"},
			} {
				Expect(createArtistWithLibrary(ctx, arr, &a, 1)).To(Succeed())
			}
		})

		It("ranks the exact transliterated match first for 'MO'", func() {
			results, err := arr.Search(ctx, "MO", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			Expect(results[0].Name).To(Equal("MØ"), "exact match via search_normalized must outrank prefix matches")
		})

		It("ranks the exact match first for the accented query 'MØ'", func() {
			results, err := arr.Search(ctx, "MØ", model.QueryOptions{Max: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(results).ToNot(BeEmpty())
			Expect(results[0].Name).To(Equal("MØ"))
		})
	})
})
