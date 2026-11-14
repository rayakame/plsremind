-- +goose Up
CREATE INDEX items_user_completed_idx ON items (user_id, completed_at DESC, id DESC) WHERE status = 'done';

-- +goose Down
DROP INDEX items_user_completed_idx;
