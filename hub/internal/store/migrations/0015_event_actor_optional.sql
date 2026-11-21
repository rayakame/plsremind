-- +goose Up
-- The hub changes things on its own too, for example when a manual reminder has had its time.
ALTER TABLE events ALTER COLUMN actor_kind DROP NOT NULL;
ALTER TABLE events ALTER COLUMN actor_id DROP NOT NULL;
ALTER TABLE events ADD CONSTRAINT events_actor_whole CHECK ((actor_kind IS NULL) = (actor_id IS NULL));

-- +goose Down
DELETE FROM events WHERE actor_kind IS NULL;
ALTER TABLE events DROP CONSTRAINT events_actor_whole;
ALTER TABLE events ALTER COLUMN actor_id SET NOT NULL;
ALTER TABLE events ALTER COLUMN actor_kind SET NOT NULL;
