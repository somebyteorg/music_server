-- +goose Up
ALTER TABLE app_user ADD COLUMN is_can_down boolean NOT NULL DEFAULT false;
ALTER TABLE app_user ALTER COLUMN password DROP NOT NULL;
ALTER TABLE app_user ALTER COLUMN password DROP DEFAULT;
UPDATE app_user SET password = NULL, is_admin = false, token_epoch = token_epoch + 1;

-- +goose Down
UPDATE app_user SET password = '' WHERE password IS NULL;
ALTER TABLE app_user ALTER COLUMN password SET DEFAULT '';
ALTER TABLE app_user ALTER COLUMN password SET NOT NULL;
ALTER TABLE app_user DROP COLUMN is_can_down;
