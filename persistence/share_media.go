package persistence

import (
	"context"
	"fmt"
	"slices"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// hydrateShares batches resources per owner and type. Every resource query runs
// as that owner, preserving library permissions even for an administrator listing
// other users' shares or an anonymous visitor opening a public link.
func (r *shareRepository) hydrateShares(ctx context.Context, shares model.Shares) error {
	if len(shares) == 0 {
		return nil
	}
	byOwner := map[string]map[string][]*model.Share{}
	var ownerIDs []string
	for i := range shares {
		share := &shares[i]
		share.Albums, share.Tracks = nil, nil
		if byOwner[share.UserID] == nil {
			byOwner[share.UserID] = map[string][]*model.Share{}
			ownerIDs = append(ownerIDs, share.UserID)
		}
		byOwner[share.UserID][share.ResourceType] = append(byOwner[share.UserID][share.ResourceType], share)
	}
	var owners model.Users
	if err := r.queryAll(ctx, Select("id", "user_name", "is_admin").From("app_user").
		Where(stringIDsFilter("id", ownerIDs)), &owners); err != nil {
		return fmt.Errorf("loading share owners: %w", err)
	}
	if len(owners) != len(ownerIDs) {
		return fmt.Errorf("loading share owners: %w", model.ErrNotFound)
	}
	slices.SortFunc(owners, func(a, b model.User) int { return strings.Compare(a.ID, b.ID) })
	for _, owner := range owners {
		ownerCtx := request.WithUser(ctx, owner)
		for _, kind := range []string{"artist", "album", "media_file", "playlist"} {
			group := byOwner[owner.ID][kind]
			if len(group) == 0 {
				continue
			}
			if err := r.loadShareGroup(ownerCtx, kind, group); err != nil {
				return fmt.Errorf("loading %s shares for owner %q: %w", kind, owner.ID, err)
			}
		}
		for kind, group := range byOwner[owner.ID] {
			if kind != "artist" && kind != "album" && kind != "media_file" && kind != "playlist" {
				log.Warn(ctx, "Unsupported Share ResourceType", "share", group[0].ID, "resourceType", kind)
			}
		}
	}
	return nil
}

type shareReferences map[string][]*model.Share

func shareResourceIDs(share *model.Share) []string {
	var ids []string
	for _, id := range strings.Split(share.ResourceIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if share.ResourceType == "playlist" && len(ids) > 1 {
		ids = ids[:1]
	}
	return ids
}

func collectShareReferences(shares []*model.Share) ([]string, shareReferences) {
	refs := shareReferences{}
	var ids []string
	for _, share := range shares {
		seen := map[string]struct{}{}
		for _, id := range shareResourceIDs(share) {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			if len(refs[id]) == 0 {
				ids = append(ids, id)
			}
			refs[id] = append(refs[id], share)
		}
	}
	return ids, refs
}

// fanOutShareRows keeps the database's ordering and adds an item once to each
// matching share, even when it has several of the requested album artists.
func fanOutShareRows[T any](rows []T, refs shareReferences, keys func(T) []string, add func(*model.Share, T)) {
	seen := map[*model.Share]int{}
	for i, row := range rows {
		for _, key := range keys(row) {
			for _, share := range refs[key] {
				if seen[share] != i+1 {
					seen[share] = i + 1
					add(share, row)
				}
			}
		}
	}
}

func participantKeys(participants model.Participants) []string {
	var ids []string
	for _, participant := range participants[model.RoleAlbumArtist] {
		ids = append(ids, participant.ID)
	}
	return ids
}

func (r *shareRepository) loadShareGroup(ctx context.Context, kind string, shares []*model.Share) error {
	ids, refs := collectShareReferences(shares)
	if len(ids) == 0 {
		return nil
	}
	if kind == "playlist" {
		return r.loadSharedPlaylists(ctx, ids, refs)
	}
	albums := NewAlbumRepository(r.db)
	media := NewMediaFileRepository(r.db)
	if kind == "media_file" {
		tracks, err := media.GetAll(ctx, model.QueryOptions{
			Filters: And{stringIDsFilter("media_file.id", ids), Eq{"media_file.missing": false}},
		})
		if err != nil {
			return err
		}
		byID := make(map[string]model.MediaFile, len(tracks))
		for _, track := range tracks {
			byID[track.ID] = track
		}
		for _, share := range shares {
			for _, id := range shareResourceIDs(share) {
				if track, exists := byID[id]; exists {
					share.Tracks = append(share.Tracks, track)
				}
			}
		}
		return nil
	}
	albumFilter, trackFilter := stringIDsFilter("album.id", ids), stringIDsFilter("media_file.album_id", ids)
	albumSort, trackSort := "", "album"
	albumKeys := func(a model.Album) []string { return []string{a.ID} }
	trackKeys := func(m model.MediaFile) []string { return []string{m.AlbumID} }
	if kind == "artist" {
		albumFilter = ParticipantIDFilter("album", ids, model.RoleAlbumArtist)
		trackFilter = ParticipantIDFilter("media_file", ids, model.RoleAlbumArtist)
		albumSort, trackSort = "artist", "artist"
		albumKeys = func(a model.Album) []string { return participantKeys(a.Participants) }
		trackKeys = func(m model.MediaFile) []string { return participantKeys(m.Participants) }
	}
	albumRows, err := albums.GetAll(ctx, model.QueryOptions{
		Filters: And{albumFilter, Eq{"album.missing": false}}, Sort: albumSort,
	})
	if err != nil {
		return err
	}
	trackRows, err := media.GetAll(ctx, model.QueryOptions{
		Filters: And{trackFilter, Eq{"media_file.missing": false}}, Sort: trackSort,
	})
	if err != nil {
		return err
	}
	fanOutShareRows(albumRows, refs, albumKeys, func(s *model.Share, a model.Album) { s.Albums = append(s.Albums, a) })
	fanOutShareRows(trackRows, refs, trackKeys, func(s *model.Share, m model.MediaFile) { s.Tracks = append(s.Tracks, m) })
	return nil
}

func (r *shareRepository) loadSharedPlaylists(ctx context.Context, ids []string, refs shareReferences) error {
	repo := NewPlaylistRepository(r.db).(*playlistRepository)
	playlists, err := repo.GetAll(ctx, model.QueryOptions{Filters: stringIDsFilter("playlist.id", ids), Sort: "id"})
	if err != nil {
		return err
	}
	var visibleIDs []string
	for i := range playlists {
		repo.refreshSmartPlaylist(ctx, &playlists[i])
		visibleIDs = append(visibleIDs, playlists[i].ID)
	}
	if len(visibleIDs) == 0 {
		return nil
	}
	tracks, err := repo.loadTracks(ctx, Select().From("playlist_tracks").
		Where(Eq{"f.missing": false}).OrderBy("playlist_tracks.playlist_id", "playlist_tracks.id"), visibleIDs...)
	if err != nil {
		return err
	}
	fanOutShareRows(tracks, refs, func(t model.PlaylistTrack) []string { return []string{t.PlaylistID} },
		func(s *model.Share, t model.PlaylistTrack) { s.Tracks = append(s.Tracks, t.MediaFile) })
	return nil
}
