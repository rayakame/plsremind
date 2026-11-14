-- +goose Up
ALTER TABLE items ADD CONSTRAINT items_done_has_completed_at CHECK ((status = 'done') = (completed_at IS NOT NULL));
CREATE INDEX items_user_completed_idx ON items (user_id, completed_at DESC, id DESC) WHERE status = 'done';

-- +goose Down
DROP INDEX items_user_completed_idx;
ALTER TABLE items DROP CONSTRAINT items_done_has_completed_at;
