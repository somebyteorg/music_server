package persistence

import (
	"context"

	"github.com/pocketbase/dbx"
)

func cleanupPluginUserReferences(ctx context.Context, database dbx.Builder, userID string) error {
	_, err := database.NewQuery(`UPDATE plugin SET users = coalesce((
        SELECT jsonb_agg(value ORDER BY ordinal) FROM jsonb_array_elements(users) WITH ORDINALITY AS entry(value,ordinal)
        WHERE value <> to_jsonb(CAST({:id} AS text))), '[]'::jsonb), updated_at = CURRENT_TIMESTAMP
        WHERE users @> jsonb_build_array(CAST({:id} AS text))`).Bind(dbx.Params{"id": userID}).WithContext(ctx).Execute()
	if err != nil {
		return err
	}
	_, err = database.NewQuery(`UPDATE plugin SET enabled = false, updated_at = CURRENT_TIMESTAMP
        WHERE enabled AND NOT all_users AND manifest #> '{permissions,users}' IS NOT NULL
        AND coalesce(jsonb_array_length(users),0) = 0`).WithContext(ctx).Execute()
	return err
}

func cleanupPluginLibraryReferences(ctx context.Context, database dbx.Builder, libraryID int) error {
	_, err := database.NewQuery(`UPDATE plugin SET libraries = coalesce((
        SELECT jsonb_agg(value ORDER BY ordinal) FROM jsonb_array_elements(libraries) WITH ORDINALITY AS entry(value,ordinal)
        WHERE value <> to_jsonb(CAST({:id} AS integer))), '[]'::jsonb), updated_at = CURRENT_TIMESTAMP
        WHERE libraries @> jsonb_build_array(CAST({:id} AS integer))`).Bind(dbx.Params{"id": libraryID}).WithContext(ctx).Execute()
	if err != nil {
		return err
	}
	_, err = database.NewQuery(`UPDATE plugin SET enabled = false, updated_at = CURRENT_TIMESTAMP
        WHERE enabled AND NOT all_libraries AND manifest #> '{permissions,library}' IS NOT NULL
        AND coalesce(jsonb_array_length(libraries),0) = 0`).WithContext(ctx).Execute()
	return err
}
