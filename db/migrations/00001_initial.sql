-- +goose Up

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE COLLATION nd_natural (provider = icu, locale = 'und-u-kn-true');

CREATE TABLE "album" (
    "id" text NOT NULL,
    "name" text DEFAULT '' NOT NULL,
    "embed_art_path" text DEFAULT '' NOT NULL,
    "album_artist" text DEFAULT '' NOT NULL,
    "min_year" integer DEFAULT 0 NOT NULL,
    "max_year" integer DEFAULT 0 NOT NULL,
    "compilation" boolean DEFAULT FALSE NOT NULL,
    "song_count" integer DEFAULT 0 NOT NULL,
    "duration" double precision DEFAULT 0 NOT NULL,
    "genre" text DEFAULT '' NOT NULL,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "full_text" text DEFAULT '',
    "album_artist_id" text DEFAULT '',
    "size" bigint DEFAULT 0 NOT NULL,
    "description" text DEFAULT '' NOT NULL,
    "small_image_url" text DEFAULT '' NOT NULL,
    "medium_image_url" text DEFAULT '' NOT NULL,
    "large_image_url" text DEFAULT '' NOT NULL,
    "external_url" text DEFAULT '' NOT NULL,
    "external_info_updated_at" timestamptz,
    "date" text DEFAULT '' NOT NULL,
    "min_original_year" integer DEFAULT 0 NOT NULL,
    "max_original_year" integer DEFAULT 0 NOT NULL,
    "original_date" text DEFAULT '' NOT NULL,
    "release_date" text DEFAULT '' NOT NULL,
    "releases" integer DEFAULT 0 NOT NULL,
    "order_album_name" text DEFAULT '' NOT NULL,
    "order_album_artist_name" text DEFAULT '' NOT NULL,
    "sort_album_name" text DEFAULT '' NOT NULL,
    "sort_album_artist_name" text DEFAULT '' NOT NULL,
    "catalog_num" text DEFAULT '' NOT NULL,
    "comment" text DEFAULT '' NOT NULL,
    "mbz_album_id" text DEFAULT '' NOT NULL,
    "mbz_album_artist_id" text DEFAULT '' NOT NULL,
    "mbz_album_type" text DEFAULT '' NOT NULL,
    "mbz_album_comment" text DEFAULT '' NOT NULL,
    "discs" jsonb DEFAULT '{}' NOT NULL,
    "library_id" integer DEFAULT 1 NOT NULL,
    "imported_at" timestamptz DEFAULT '0001-01-01 00:00:00+00' NOT NULL,
    "missing" boolean DEFAULT false NOT NULL,
    "mbz_release_group_id" text DEFAULT '' NOT NULL,
    "tags" jsonb DEFAULT '{}' NOT NULL,
    "participants" jsonb DEFAULT '{}' NOT NULL,
    "folder_ids" jsonb DEFAULT '[]' NOT NULL,
    "explicit_status" text DEFAULT '' NOT NULL,
    "average_rating" numeric(5,2) DEFAULT 0 NOT NULL,
    "search_participants" text DEFAULT '' NOT NULL,
    "search_normalized" text DEFAULT '' NOT NULL,
    "rg_album_gain" double precision,
    "rg_album_peak" double precision,
    PRIMARY KEY ("id")
);

CREATE TABLE "album_artists" (
    "album_id" text NOT NULL,
    "artist_id" text NOT NULL,
    "role" text DEFAULT '' NOT NULL,
    "sub_role" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("artist_id", "album_id", "role", "sub_role")
);

CREATE TABLE "album_tags" (
    "album_id" text NOT NULL,
    "tag_id" text NOT NULL,
    PRIMARY KEY ("album_id", "tag_id")
);

CREATE TABLE "annotation" (
    "user_id" text NOT NULL,
    "item_id" text DEFAULT '' NOT NULL,
    "item_type" text DEFAULT '' NOT NULL,
    "play_count" integer DEFAULT 0,
    "play_date" timestamptz,
    "rating" integer DEFAULT 0,
    "starred" boolean DEFAULT FALSE NOT NULL,
    "starred_at" timestamptz,
    "rated_at" timestamptz,
    PRIMARY KEY ("user_id", "item_id", "item_type")
);

CREATE TABLE "artist" (
    "id" text NOT NULL,
    "name" text DEFAULT '' NOT NULL,
    "full_text" text DEFAULT '',
    "biography" text DEFAULT '' NOT NULL,
    "small_image_url" text DEFAULT '' NOT NULL,
    "medium_image_url" text DEFAULT '' NOT NULL,
    "large_image_url" text DEFAULT '' NOT NULL,
    "external_url" text DEFAULT '' NOT NULL,
    "external_info_updated_at" timestamptz,
    "order_artist_name" text DEFAULT '' NOT NULL,
    "sort_artist_name" text DEFAULT '' NOT NULL,
    "mbz_artist_id" text DEFAULT '' NOT NULL,
    "missing" boolean DEFAULT false NOT NULL,
    "similar_artists" jsonb DEFAULT '[]' NOT NULL,
    "updated_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "created_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "average_rating" numeric(5,2) DEFAULT 0 NOT NULL,
    "search_normalized" text DEFAULT '' NOT NULL,
    "uploaded_image" text DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE TABLE "artwork" (
    "hash" text NOT NULL,
    "mime" text NOT NULL,
    "width" integer DEFAULT 0 NOT NULL,
    "height" integer DEFAULT 0 NOT NULL,
    "size_bytes" bigint DEFAULT 0 NOT NULL,
    "blur_hash" text DEFAULT '' NOT NULL,
    "thumb_hash" text DEFAULT '' NOT NULL,
    "dominant_color" text DEFAULT '' NOT NULL,
    "created_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    PRIMARY KEY ("hash")
);

CREATE TABLE "artwork_queue" (
    "item_kind" text NOT NULL,
    "item_id" text NOT NULL,
    "image_type" text DEFAULT 'primary' NOT NULL,
    "priority" integer DEFAULT 0 NOT NULL,
    "attempts" integer DEFAULT 0 NOT NULL,
    "retry_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "enqueued_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "trace" jsonb DEFAULT '[]' NOT NULL,
    PRIMARY KEY ("item_kind", "item_id", "image_type")
);

CREATE TABLE "bookmark" (
    "user_id" text NOT NULL,
    "item_id" text NOT NULL,
    "item_type" text NOT NULL,
    "comment" text,
    "position" bigint,
    "changed_by" text,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    PRIMARY KEY ("user_id", "item_id", "item_type")
);

CREATE TABLE "folder" (
    "id" text NOT NULL,
    "library_id" integer NOT NULL,
    "path" text DEFAULT '' NOT NULL,
    "name" text DEFAULT '' NOT NULL,
    "missing" boolean DEFAULT false NOT NULL,
    "parent_id" text DEFAULT '' NOT NULL,
    "num_audio_files" integer DEFAULT 0 NOT NULL,
    "num_playlists" integer DEFAULT 0 NOT NULL,
    "image_files" jsonb DEFAULT '[]' NOT NULL,
    "images_updated_at" timestamptz DEFAULT '0001-01-01 00:00:00+00' NOT NULL,
    "updated_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "created_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "hash" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "item_artwork" (
    "item_kind" text NOT NULL,
    "item_id" text NOT NULL,
    "image_type" text DEFAULT 'primary' NOT NULL,
    "hash" text DEFAULT '' NOT NULL,
    "source" text DEFAULT '' NOT NULL,
    "source_path" text DEFAULT '' NOT NULL,
    "ref_mtime" bigint DEFAULT 0 NOT NULL,
    "attempted_at" timestamptz,
    "updated_at" timestamptz,
    "trace" jsonb DEFAULT '[]' NOT NULL,
    "last_failure" jsonb,
    PRIMARY KEY ("item_kind", "item_id", "image_type")
);

CREATE TABLE "library" (
    "id" integer GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    "name" text NOT NULL,
    "path" text NOT NULL,
    "remote_path" text DEFAULT '',
    "last_scan_at" timestamptz DEFAULT '0001-01-01 00:00:00+00' NOT NULL,
    "updated_at" timestamptz DEFAULT current_timestamp NOT NULL,
    "created_at" timestamptz DEFAULT current_timestamp NOT NULL,
    "last_scan_started_at" timestamptz DEFAULT '0001-01-01 00:00:00+00' NOT NULL,
    "full_scan_in_progress" boolean DEFAULT false NOT NULL,
    "total_songs" integer DEFAULT 0 NOT NULL,
    "total_albums" integer DEFAULT 0 NOT NULL,
    "total_artists" integer DEFAULT 0 NOT NULL,
    "total_folders" integer DEFAULT 0 NOT NULL,
    "total_files" integer DEFAULT 0 NOT NULL,
    "total_missing_files" integer DEFAULT 0 NOT NULL,
    "total_size" bigint DEFAULT 0 NOT NULL,
    "total_duration" double precision DEFAULT 0,
    "default_new_users" boolean DEFAULT false,
    "pid_album" text DEFAULT '' NOT NULL,
    "pid_track" text DEFAULT '' NOT NULL,
    "scanned_pid_album" text DEFAULT '' NOT NULL,
    "scanned_pid_track" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id"),
    UNIQUE ("path"),
    UNIQUE ("name")
);

CREATE TABLE "library_artist" (
    "library_id" integer DEFAULT 1 NOT NULL,
    "artist_id" text NOT NULL,
    "stats" jsonb DEFAULT '{}' NOT NULL,
    PRIMARY KEY ("library_id", "artist_id")
);

CREATE TABLE "library_tag" (
    "tag_id" text NOT NULL,
    "library_id" integer NOT NULL,
    "album_count" integer DEFAULT 0 NOT NULL,
    "media_file_count" integer DEFAULT 0 NOT NULL,
    PRIMARY KEY ("tag_id", "library_id")
);

CREATE TABLE "media_file" (
    "id" text NOT NULL,
    "path" text DEFAULT '' NOT NULL,
    "title" text DEFAULT '' NOT NULL,
    "album" text DEFAULT '' NOT NULL,
    "artist" text DEFAULT '' NOT NULL,
    "artist_id" text DEFAULT '' NOT NULL,
    "album_artist" text DEFAULT '' NOT NULL,
    "album_id" text DEFAULT '' NOT NULL,
    "has_cover_art" boolean DEFAULT FALSE NOT NULL,
    "track_number" integer DEFAULT 0 NOT NULL,
    "disc_number" integer DEFAULT 0 NOT NULL,
    "year" integer DEFAULT 0 NOT NULL,
    "size" bigint DEFAULT 0 NOT NULL,
    "suffix" text DEFAULT '' NOT NULL,
    "duration" double precision DEFAULT 0 NOT NULL,
    "bit_rate" integer DEFAULT 0 NOT NULL,
    "genre" text DEFAULT '' NOT NULL,
    "compilation" boolean DEFAULT FALSE NOT NULL,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "full_text" text DEFAULT '',
    "album_artist_id" text DEFAULT '',
    "date" text DEFAULT '' NOT NULL,
    "original_year" integer DEFAULT 0 NOT NULL,
    "original_date" text DEFAULT '' NOT NULL,
    "release_year" integer DEFAULT 0 NOT NULL,
    "release_date" text DEFAULT '' NOT NULL,
    "order_album_name" text DEFAULT '' NOT NULL,
    "order_album_artist_name" text DEFAULT '' NOT NULL,
    "order_artist_name" text DEFAULT '' NOT NULL,
    "sort_album_name" text DEFAULT '' NOT NULL,
    "sort_artist_name" text DEFAULT '' NOT NULL,
    "sort_album_artist_name" text DEFAULT '' NOT NULL,
    "sort_title" text DEFAULT '' NOT NULL,
    "disc_subtitle" text DEFAULT '' NOT NULL,
    "catalog_num" text DEFAULT '' NOT NULL,
    "comment" text DEFAULT '' NOT NULL,
    "order_title" text DEFAULT '' NOT NULL,
    "mbz_recording_id" text DEFAULT '' NOT NULL,
    "mbz_album_id" text DEFAULT '' NOT NULL,
    "mbz_artist_id" text DEFAULT '' NOT NULL,
    "mbz_album_artist_id" text DEFAULT '' NOT NULL,
    "mbz_album_type" text DEFAULT '' NOT NULL,
    "mbz_album_comment" text DEFAULT '' NOT NULL,
    "mbz_release_track_id" text DEFAULT '' NOT NULL,
    "channels" integer DEFAULT 0 NOT NULL,
    "lyrics" jsonb DEFAULT '[]' NOT NULL,
    "sample_rate" integer DEFAULT 0 NOT NULL,
    "library_id" integer DEFAULT 1 NOT NULL,
    "folder_id" text DEFAULT '' NOT NULL,
    "pid" text DEFAULT '' NOT NULL,
    "missing" boolean DEFAULT false NOT NULL,
    "mbz_release_group_id" text DEFAULT '' NOT NULL,
    "tags" jsonb DEFAULT '{}' NOT NULL,
    "participants" jsonb DEFAULT '{}' NOT NULL,
    "explicit_status" text DEFAULT '' NOT NULL,
    "birth_time" timestamptz DEFAULT current_timestamp NOT NULL,
    "rg_album_gain" double precision,
    "rg_album_peak" double precision,
    "rg_track_gain" double precision,
    "rg_track_peak" double precision,
    "average_rating" numeric(5,2) DEFAULT 0 NOT NULL,
    "search_participants" text DEFAULT '' NOT NULL,
    "search_normalized" text DEFAULT '' NOT NULL,
    "codec" text DEFAULT '' NOT NULL,
    "probe_data" text DEFAULT '' NOT NULL,
    "bpm" integer,
    "bit_depth" integer,
    PRIMARY KEY ("id")
);

CREATE TABLE "media_file_artists" (
    "media_file_id" text NOT NULL,
    "artist_id" text NOT NULL,
    "role" text DEFAULT '' NOT NULL,
    "sub_role" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("artist_id", "media_file_id", "role", "sub_role")
);

CREATE TABLE "media_file_tags" (
    "media_file_id" text NOT NULL,
    "tag_id" text NOT NULL,
    PRIMARY KEY ("media_file_id", "tag_id")
);

CREATE TABLE "player" (
    "id" text NOT NULL,
    "name" text NOT NULL,
    "user_agent" text,
    "user_id" text NOT NULL,
    "client" text NOT NULL,
    "ip" text,
    "last_seen" timestamptz,
    "max_bit_rate" integer DEFAULT 0,
    "transcoding_id" text,
    "report_real_path" boolean DEFAULT FALSE NOT NULL,
    "scrobble_enabled" boolean DEFAULT true,
    "api_key_hash" text DEFAULT null,
    PRIMARY KEY ("id")
);

CREATE TABLE "playlist" (
    "id" text NOT NULL,
    "name" text DEFAULT '' NOT NULL,
    "comment" text DEFAULT '' NOT NULL,
    "duration" double precision DEFAULT 0 NOT NULL,
    "song_count" integer DEFAULT 0 NOT NULL,
    "public" boolean DEFAULT FALSE NOT NULL,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "path" text DEFAULT '' NOT NULL,
    "sync" boolean DEFAULT false NOT NULL,
    "size" bigint DEFAULT 0 NOT NULL,
    "rules" text,
    "evaluated_at" timestamptz,
    "owner_id" text NOT NULL,
    "uploaded_image" text DEFAULT '',
    "external_image_url" text DEFAULT '',
    "average_rating" numeric(5,2) DEFAULT 0 NOT NULL,
    "imported_hash" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "playlist_fields" (
    "field" text NOT NULL,
    "playlist_id" text NOT NULL,
    PRIMARY KEY ("playlist_id", "field")
);

CREATE TABLE "playlist_tracks" (
    "id" integer DEFAULT 0 NOT NULL,
    "playlist_id" text NOT NULL,
    "media_file_id" text NOT NULL,
    PRIMARY KEY ("playlist_id", "id")
);

CREATE TABLE "playqueue" (
    "id" text NOT NULL,
    "user_id" text NOT NULL,
    "current" integer DEFAULT 0 NOT NULL,
    "changed_by" text,
    "items" text,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "position" bigint,
    PRIMARY KEY ("id"),
    UNIQUE (user_id)
);

CREATE TABLE "plugin" (
    "id" text NOT NULL,
    "path" text NOT NULL,
    "manifest" jsonb NOT NULL,
    "config" jsonb,
    "users" jsonb,
    "all_users" boolean DEFAULT false NOT NULL,
    "libraries" jsonb,
    "all_libraries" boolean DEFAULT false NOT NULL,
    "enabled" boolean DEFAULT false NOT NULL,
    "last_error" text,
    "sha256" text NOT NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "allow_write_access" boolean DEFAULT false NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "property" (
    "id" text NOT NULL,
    "value" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "radio" (
    "id" text NOT NULL,
    "name" text NOT NULL,
    "stream_url" text NOT NULL,
    "home_page_url" text DEFAULT '' NOT NULL,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "uploaded_image" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "scrobble_buffer" (
    "user_id" text NOT NULL,
    "service" text NOT NULL,
    "media_file_id" text NOT NULL,
    "play_time" timestamptz NOT NULL,
    "enqueue_time" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "id" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id"),
    UNIQUE ("user_id", "service", "media_file_id", "play_time")
);

CREATE TABLE "scrobbles" (
    "id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    "media_file_id" text NOT NULL,
    "user_id" text NOT NULL,
    "submission_time" bigint NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "share" (
    "id" text NOT NULL,
    "expires_at" timestamptz,
    "last_visited_at" timestamptz,
    "resource_ids" text NOT NULL,
    "created_at" timestamptz,
    "updated_at" timestamptz,
    "user_id" text NOT NULL,
    "downloadable" boolean DEFAULT false NOT NULL,
    "description" text DEFAULT '' NOT NULL,
    "resource_type" text DEFAULT '' NOT NULL,
    "contents" text DEFAULT '' NOT NULL,
    "format" text DEFAULT '' NOT NULL,
    "max_bit_rate" integer DEFAULT 0 NOT NULL,
    "visit_count" integer DEFAULT 0 NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "tag" (
    "id" text NOT NULL,
    "tag_name" text DEFAULT '' NOT NULL,
    "tag_value" text DEFAULT '' NOT NULL,
    PRIMARY KEY ("id"),
    UNIQUE ("tag_name", "tag_value")
);

CREATE TABLE "transcoding" (
    "id" text NOT NULL,
    "name" text NOT NULL,
    "target_format" text NOT NULL,
    "command" text DEFAULT '' NOT NULL,
    "default_bit_rate" integer DEFAULT 192,
    PRIMARY KEY ("id"),
    UNIQUE ("target_format"),
    UNIQUE ("name")
);

CREATE TABLE "app_user" (
    "id" text NOT NULL,
    "user_name" text DEFAULT '' NOT NULL,
    "name" text DEFAULT '' NOT NULL,
    "email" text DEFAULT '' NOT NULL,
    "password" text DEFAULT '' NOT NULL,
    "is_admin" boolean DEFAULT FALSE NOT NULL,
    "last_login_at" timestamptz,
    "last_access_at" timestamptz,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "scrobble_filter" text DEFAULT '' NOT NULL,
    "token_epoch" integer DEFAULT 0 NOT NULL,
    PRIMARY KEY ("id")
);

CREATE TABLE "user_library" (
    "user_id" text NOT NULL,
    "library_id" integer NOT NULL,
    PRIMARY KEY ("user_id", "library_id")
);

CREATE TABLE "user_props" (
    "user_id" text NOT NULL,
    "key" text NOT NULL,
    "value" text,
    PRIMARY KEY ("user_id", "key")
);

ALTER TABLE "album" ADD CONSTRAINT "album_fk_0" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "album_artists" ADD CONSTRAINT "album_artists_fk_0" FOREIGN KEY ("artist_id") REFERENCES "artist" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "album_artists" ADD CONSTRAINT "album_artists_fk_1" FOREIGN KEY ("album_id") REFERENCES "album" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "album_tags" ADD CONSTRAINT "album_tags_fk_0" FOREIGN KEY ("tag_id") REFERENCES "tag" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "album_tags" ADD CONSTRAINT "album_tags_fk_1" FOREIGN KEY ("album_id") REFERENCES "album" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "annotation" ADD CONSTRAINT "annotation_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "bookmark" ADD CONSTRAINT "bookmark_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "folder" ADD CONSTRAINT "folder_fk_0" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "library_artist" ADD CONSTRAINT "library_artist_fk_0" FOREIGN KEY ("artist_id") REFERENCES "artist" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "library_artist" ADD CONSTRAINT "library_artist_fk_1" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "library_tag" ADD CONSTRAINT "library_tag_fk_0" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "library_tag" ADD CONSTRAINT "library_tag_fk_1" FOREIGN KEY ("tag_id") REFERENCES "tag" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "media_file" ADD CONSTRAINT "media_file_fk_0" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "media_file_artists" ADD CONSTRAINT "media_file_artists_fk_0" FOREIGN KEY ("artist_id") REFERENCES "artist" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "media_file_artists" ADD CONSTRAINT "media_file_artists_fk_1" FOREIGN KEY ("media_file_id") REFERENCES "media_file" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "media_file_tags" ADD CONSTRAINT "media_file_tags_fk_0" FOREIGN KEY ("tag_id") REFERENCES "tag" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "media_file_tags" ADD CONSTRAINT "media_file_tags_fk_1" FOREIGN KEY ("media_file_id") REFERENCES "media_file" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "player" ADD CONSTRAINT "player_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "playlist" ADD CONSTRAINT "playlist_fk_0" FOREIGN KEY ("owner_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "playlist_fields" ADD CONSTRAINT "playlist_fields_fk_0" FOREIGN KEY ("playlist_id") REFERENCES "playlist" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "playlist_tracks" ADD CONSTRAINT "playlist_tracks_fk_0" FOREIGN KEY ("playlist_id") REFERENCES "playlist" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "playqueue" ADD CONSTRAINT "playqueue_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "scrobble_buffer" ADD CONSTRAINT "scrobble_buffer_fk_0" FOREIGN KEY ("media_file_id") REFERENCES "media_file" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "scrobble_buffer" ADD CONSTRAINT "scrobble_buffer_fk_1" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "scrobbles" ADD CONSTRAINT "scrobbles_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "scrobbles" ADD CONSTRAINT "scrobbles_fk_1" FOREIGN KEY ("media_file_id") REFERENCES "media_file" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "share" ADD CONSTRAINT "share_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE "user_library" ADD CONSTRAINT "user_library_fk_0" FOREIGN KEY ("library_id") REFERENCES "library" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "user_library" ADD CONSTRAINT "user_library_fk_1" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE "user_props" ADD CONSTRAINT "user_props_fk_0" FOREIGN KEY ("user_id") REFERENCES "app_user" ("id") ON UPDATE CASCADE ON DELETE CASCADE;

CREATE INDEX "album_updated_at" ON "album" ("updated_at", "id");

CREATE INDEX "album_created_at" ON "album" ("created_at", "id");

CREATE INDEX "album_mbz_album_id" ON "album" ("mbz_album_id");

CREATE INDEX "album_mbz_release_group_id" ON "album" ("mbz_release_group_id");

CREATE INDEX "album_imported_at" ON "album" ("imported_at");

CREATE INDEX "album_size" ON "album" ("size");

CREATE INDEX "album_order_album_name" ON "album" (lower("order_album_name"));

CREATE INDEX "album_order_album_artist_name" ON "album" (lower("order_album_artist_name"));

CREATE INDEX "album_name" ON "album" ("name");

CREATE INDEX "album_min_year" ON "album" ("min_year");

CREATE INDEX "album_mbz_album_type" ON "album" ("mbz_album_type");

CREATE INDEX "album_max_year" ON "album" ("max_year");

CREATE INDEX "album_genre" ON "album" ("genre");

CREATE INDEX "album_artist_album_id" ON "album" ("album_artist_id");

CREATE INDEX "album_alphabetical_by_artist" ON "album" ("compilation", lower("order_album_artist_name"), lower("order_album_name"));

CREATE INDEX "album_artists_role" ON "album_artists" ("role");

CREATE INDEX "album_artists_album_id" ON "album_artists" ("album_id");

CREATE INDEX "album_tags_tag_id" ON "album_tags" ("tag_id");

CREATE INDEX "annotation_starred_at" ON "annotation" ("starred_at");

CREATE INDEX "annotation_starred" ON "annotation" ("starred");

CREATE INDEX "annotation_rating" ON "annotation" ("rating");

CREATE INDEX "annotation_play_date" ON "annotation" ("play_date");

CREATE INDEX "annotation_play_count" ON "annotation" ("play_count");

CREATE INDEX annotation_item ON annotation (item_type, item_id);

CREATE INDEX "artist_mbz_artist_id" ON "artist" ("mbz_artist_id");

CREATE INDEX "artist_updated_at" ON "artist" ("updated_at");

CREATE INDEX "artist_order_artist_name" ON "artist" (lower("order_artist_name"));

CREATE INDEX "artist_name" ON "artist" ("name");

CREATE INDEX "ix_artwork_queue_drain" ON "artwork_queue" ("item_kind", "priority" DESC, "enqueued_at", "retry_at");

CREATE INDEX "folder_parent_id" ON "folder" ("parent_id");

CREATE INDEX "ix_item_artwork_hash" ON "item_artwork" ("hash");

CREATE INDEX "idx_library_tag_library_id" ON "library_tag" ("library_id");

CREATE INDEX "media_file_missing_library_order_title" ON "media_file" ("missing", "library_id", lower("order_title"), "id");

CREATE INDEX "media_file_album_artist_sort" ON "media_file" (lower("order_album_artist_name"), lower("order_album_name"), "release_date", "disc_number", "track_number");

CREATE INDEX "media_file_artist_sort" ON "media_file" (lower("order_artist_name"), lower("order_album_name"), "release_date", "disc_number", "track_number");

CREATE INDEX "media_file_album_sort" ON "media_file" (lower("order_album_name"), "album_id", "disc_number", "track_number", lower("order_artist_name"), "title");

CREATE INDEX "media_file_updated_at" ON "media_file" ("updated_at", "id");

CREATE INDEX "media_file_created_at" ON "media_file" ("created_at", "id");

CREATE INDEX "media_file_bpm" ON "media_file" ("bpm");

CREATE INDEX "media_file_missing_library_id" ON "media_file" ("missing", "library_id");

CREATE INDEX "media_file_codec" ON "media_file" ("codec");

CREATE INDEX "media_file_mbz_release_track_id" ON "media_file" ("mbz_release_track_id");

CREATE INDEX "media_file_pid" ON "media_file" ("pid");

CREATE INDEX "media_file_folder_id" ON "media_file" ("folder_id");

CREATE INDEX "media_file_year" ON "media_file" ("year");

CREATE INDEX "media_file_track_number" ON "media_file" ("disc_number", "track_number");

CREATE INDEX "media_file_title" ON "media_file" ("title");

CREATE INDEX "media_file_sample_rate" ON "media_file" ("sample_rate");

CREATE INDEX "media_file_path_lower" ON "media_file" (lower("path"));

CREATE INDEX "media_file_path" ON "media_file" ("path");

CREATE INDEX "media_file_order_title" ON "media_file" (lower("order_title"));

CREATE INDEX "media_file_mbz_track_id" ON "media_file" ("mbz_recording_id");

CREATE INDEX "media_file_genre" ON "media_file" ("genre");

CREATE INDEX "media_file_duration" ON "media_file" ("duration");

CREATE INDEX "media_file_channels" ON "media_file" ("channels");

CREATE INDEX "media_file_artist_id" ON "media_file" ("artist_id");

CREATE INDEX "media_file_artist_album_id" ON "media_file" ("album_artist_id");

CREATE INDEX "media_file_album_id" ON "media_file" ("album_id");

CREATE INDEX "media_file_artists_media_file_id_role" ON "media_file_artists" ("media_file_id", "role");

CREATE INDEX "media_file_artists_role" ON "media_file_artists" ("role");

CREATE INDEX "media_file_tags_tag_id" ON "media_file_tags" ("tag_id");

CREATE UNIQUE INDEX "player_api_key_hash" ON "player" ("api_key_hash");

CREATE INDEX "player_name" ON "player" ("name");

CREATE INDEX "player_match" ON "player" ("client", "user_agent", "user_id");

CREATE INDEX "playlist_size" ON "playlist" ("size");

CREATE INDEX "playlist_evaluated_at" ON "playlist" ("evaluated_at");

CREATE INDEX "playlist_updated_at" ON "playlist" ("updated_at");

CREATE INDEX "playlist_created_at" ON "playlist" ("created_at");

CREATE INDEX "playlist_name" ON "playlist" (lower("name"));

CREATE UNIQUE INDEX "playlist_fields_idx" ON "playlist_fields" ("field", "playlist_id");

CREATE INDEX "scrobbles_user_time" ON "scrobbles" ("user_id", "submission_time");

CREATE INDEX "idx_user_library_library_id" ON "user_library" ("library_id");

CREATE UNIQUE INDEX app_user_user_name_ci ON app_user (lower(user_name));

CREATE UNIQUE INDEX radio_name_ci ON radio (lower(name));

CREATE INDEX album_tags_gin ON album USING gin (tags jsonb_path_ops);

CREATE INDEX media_file_tags_gin ON media_file USING gin (tags jsonb_path_ops);

CREATE INDEX artist_preferred_artist_name ON artist (lower(coalesce(nullif(sort_artist_name, ''), order_artist_name)));

CREATE INDEX artist_natural_artist_name ON artist ((lower(coalesce(nullif(sort_artist_name, ''), order_artist_name)) COLLATE nd_natural));

CREATE INDEX album_preferred_album_name ON album (lower(coalesce(nullif(sort_album_name, ''), order_album_name)));

CREATE INDEX album_natural_album_name ON album ((lower(coalesce(nullif(sort_album_name, ''), order_album_name)) COLLATE nd_natural));

CREATE INDEX album_preferred_album_artist_name ON album (lower(coalesce(nullif(sort_album_artist_name, ''), order_album_artist_name)));

CREATE INDEX album_natural_album_artist_name ON album ((lower(coalesce(nullif(sort_album_artist_name, ''), order_album_artist_name)) COLLATE nd_natural));

CREATE INDEX media_file_preferred_title ON media_file (lower(coalesce(nullif(sort_title, ''), order_title)));

CREATE INDEX media_file_natural_title ON media_file ((lower(coalesce(nullif(sort_title, ''), order_title)) COLLATE nd_natural));

CREATE INDEX media_file_preferred_artist_name ON media_file (lower(coalesce(nullif(sort_artist_name, ''), order_artist_name)));

CREATE INDEX media_file_natural_artist_name ON media_file ((lower(coalesce(nullif(sort_artist_name, ''), order_artist_name)) COLLATE nd_natural));

CREATE INDEX media_file_preferred_album_name ON media_file (lower(coalesce(nullif(sort_album_name, ''), order_album_name)));

CREATE INDEX media_file_natural_album_name ON media_file ((lower(coalesce(nullif(sort_album_name, ''), order_album_name)) COLLATE nd_natural));

CREATE INDEX media_file_preferred_album_artist_name ON media_file (lower(coalesce(nullif(sort_album_artist_name, ''), order_album_artist_name)));

CREATE INDEX media_file_natural_album_artist_name ON media_file ((lower(coalesce(nullif(sort_album_artist_name, ''), order_album_artist_name)) COLLATE nd_natural));

ALTER TABLE artist ADD COLUMN search_document tsvector GENERATED ALWAYS AS (setweight(to_tsvector('simple', coalesce(search_normalized, '')), 'A') || setweight(to_tsvector('simple', coalesce(sort_artist_name, '')), 'B') || setweight(to_tsvector('simple', coalesce(full_text, '')), 'C')) STORED;

CREATE INDEX artist_search_document_idx ON artist USING gin (search_document);

CREATE INDEX artist_search_text_idx ON artist USING gin (full_text gin_trgm_ops);

CREATE INDEX artist_name_trgm_idx ON artist USING gin (name gin_trgm_ops);

ALTER TABLE album ADD COLUMN search_document tsvector GENERATED ALWAYS AS (setweight(to_tsvector('simple', coalesce(search_normalized, '')), 'A') || setweight(to_tsvector('simple', coalesce(search_participants, '')), 'B') || setweight(to_tsvector('simple', coalesce(full_text, '')), 'C')) STORED;

CREATE INDEX album_search_document_idx ON album USING gin (search_document);

CREATE INDEX album_search_text_idx ON album USING gin (full_text gin_trgm_ops);

CREATE INDEX album_name_trgm_idx ON album USING gin (name gin_trgm_ops);

CREATE INDEX album_album_artist_trgm_idx ON album USING gin (album_artist gin_trgm_ops);

ALTER TABLE media_file ADD COLUMN search_document tsvector GENERATED ALWAYS AS (setweight(to_tsvector('simple', coalesce(search_normalized, '')), 'A') || setweight(to_tsvector('simple', coalesce(search_participants, '')), 'B') || setweight(to_tsvector('simple', coalesce(full_text, '')), 'C')) STORED;

CREATE INDEX media_file_search_document_idx ON media_file USING gin (search_document);

CREATE INDEX media_file_search_text_idx ON media_file USING gin (full_text gin_trgm_ops);

CREATE INDEX media_file_title_trgm_idx ON media_file USING gin (title gin_trgm_ops);

CREATE INDEX media_file_album_trgm_idx ON media_file USING gin (album gin_trgm_ops);

CREATE INDEX media_file_artist_trgm_idx ON media_file USING gin (artist gin_trgm_ops);

CREATE INDEX media_file_album_artist_trgm_idx ON media_file USING gin (album_artist gin_trgm_ops);

CREATE TABLE plugin_kvstore (
    plugin_id text NOT NULL REFERENCES plugin(id) ON DELETE CASCADE,
    key text NOT NULL,
    value bytea NOT NULL,
    size bigint NOT NULL CHECK (size >= 0),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamptz,
    PRIMARY KEY (plugin_id, key)
);

CREATE INDEX plugin_kvstore_expiry ON plugin_kvstore (plugin_id, expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE plugin_queue (
    plugin_id text NOT NULL REFERENCES plugin(id) ON DELETE CASCADE,
    name text NOT NULL,
    concurrency integer NOT NULL DEFAULT 1,
    max_retries integer NOT NULL DEFAULT 0,
    backoff_ms bigint NOT NULL DEFAULT 1000,
    delay_ms bigint NOT NULL DEFAULT 0,
    retention_ms bigint NOT NULL DEFAULT 3600000,
    PRIMARY KEY (plugin_id, name)
);

CREATE TABLE plugin_task (
    plugin_id text NOT NULL,
    id text NOT NULL,
    queue_name text NOT NULL,
    payload bytea NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed','cancelled')),
    attempt integer NOT NULL DEFAULT 0,
    max_retries integer NOT NULL,
    next_run_at bigint NOT NULL,
    created_at bigint NOT NULL,
    updated_at bigint NOT NULL,
    message text NOT NULL DEFAULT '',
    PRIMARY KEY (plugin_id, id),
    FOREIGN KEY (plugin_id, queue_name) REFERENCES plugin_queue(plugin_id, name) ON DELETE CASCADE
);

CREATE INDEX plugin_task_dequeue ON plugin_task (plugin_id, queue_name, status, next_run_at, created_at);

-- PostgreSQL does not create indexes on the referencing side of foreign keys.
CREATE INDEX album_library_id ON album (library_id);
CREATE INDEX folder_library_id ON folder (library_id);
CREATE INDEX library_artist_artist_id ON library_artist (artist_id);
CREATE INDEX media_file_library_id ON media_file (library_id);
CREATE INDEX player_user_id ON player (user_id);
CREATE INDEX playlist_owner_id ON playlist (owner_id);
CREATE INDEX playlist_tracks_media_file_id ON playlist_tracks (media_file_id);
CREATE INDEX scrobble_buffer_media_file_id ON scrobble_buffer (media_file_id);
CREATE INDEX scrobbles_media_file_id ON scrobbles (media_file_id);
CREATE INDEX share_user_id ON share (user_id);

INSERT INTO library (id, name, path, default_new_users) VALUES (1, 'Music Library', '', true);

SELECT setval(pg_get_serial_sequence('library', 'id'), 1);

INSERT INTO transcoding (id, name, target_format, command, default_bit_rate) VALUES ('2UweKTSJt5z4pOryb5OhsZ', 'mp3 audio', 'mp3', 'ffmpeg -ss %t -i %s -map 0:a:0 -map_metadata 0 -map_metadata 0:s:a:0 -b:a %bk -v 0 -f mp3 -', 192);

INSERT INTO transcoding (id, name, target_format, command, default_bit_rate) VALUES ('1oR82RbqvxItx98wyZSZKC', 'opus audio', 'opus', 'ffmpeg -ss %t -i %s -map 0:a:0 -map_metadata 0 -map_metadata 0:s:a:0 -b:a %bk -v 0 -c:a libopus -f opus -', 128);

INSERT INTO transcoding (id, name, target_format, command, default_bit_rate) VALUES ('1TIRUN5BSl5phnc4ulQMPb', 'aac audio', 'aac', 'ffmpeg -ss %t -i %s -map 0:a:0 -map_metadata 0 -map_metadata 0:s:a:0 -b:a %bk -v 0 -c:a aac -f adts -', 256);

INSERT INTO transcoding (id, name, target_format, command, default_bit_rate) VALUES ('1KaJbV82MHqDUrOL0f2rDJ', 'flac audio', 'flac', 'ffmpeg -ss %t -i %s -map 0:a:0 -map_metadata 0 -map_metadata 0:s:a:0 -v 0 -c:a flac -f flac -', 0);

-- +goose Down

DROP TABLE "plugin_task", "plugin_queue", "plugin_kvstore", "album", "album_artists", "album_tags", "annotation", "artist", "artwork", "artwork_queue", "bookmark", "folder", "item_artwork", "library", "library_artist", "library_tag", "media_file", "media_file_artists", "media_file_tags", "player", "playlist", "playlist_fields", "playlist_tracks", "playqueue", "plugin", "property", "radio", "scrobble_buffer", "scrobbles", "share", "tag", "transcoding", "app_user", "user_library", "user_props" CASCADE;

DROP COLLATION nd_natural;
