-- +goose Up
ALTER TABLE users
    ALTER COLUMN quiet_start DROP NOT NULL,
    ALTER COLUMN quiet_start DROP DEFAULT,
    ALTER COLUMN quiet_end DROP NOT NULL,
    ALTER COLUMN quiet_end DROP DEFAULT;
-- Nobody chose the old default of 22:00 to 08:00, so quiet hours start out off for everyone.
UPDATE users SET quiet_start = NULL, quiet_end = NULL;
ALTER TABLE users ADD CONSTRAINT users_quiet_both CHECK ((quiet_start IS NULL) = (quiet_end IS NULL));

ALTER TABLE connections
    ADD COLUMN allowed_intents text[] NOT NULL DEFAULT '{users,lists,items,plans,deliveries}';

-- NULL means "no choice made": enabled then follows the linked identity, remind is on.
ALTER TABLE connection_grants
    ALTER COLUMN enabled DROP NOT NULL,
    ADD COLUMN remind boolean;

ALTER TABLE personal_tokens ADD COLUMN remind boolean NOT NULL DEFAULT false;

-- Every reminder that is still ahead for an open item. Rewritten whenever something
-- that decides the times changes; rows whose time has come belong to the ticker.
CREATE TABLE scheduled_reminders (
    id uuid PRIMARY KEY,
    item_id uuid NOT NULL REFERENCES items ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('before', 'due', 'follow_up', 'manual')),
    offset_seconds integer,
    manual_id uuid REFERENCES manual_reminders ON DELETE CASCADE,
    fire_at timestamptz NOT NULL,
    scheduled_for timestamptz NOT NULL
);
CREATE INDEX scheduled_reminders_fire_at_idx ON scheduled_reminders (fire_at);
CREATE INDEX scheduled_reminders_item_id_idx ON scheduled_reminders (item_id);

-- What the hub did when a reminder's time came.
CREATE TABLE reminder_log (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    item_id uuid NOT NULL REFERENCES items ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('before', 'due', 'follow_up', 'manual')),
    offset_seconds integer,
    scheduled_for timestamptz NOT NULL,
    -- When it was supposed to go out, which quiet hours can put after scheduled_for.
    fire_at timestamptz NOT NULL,
    fired_at timestamptz NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('fired', 'missed', 'superseded'))
);
CREATE INDEX reminder_log_user_idx ON reminder_log (user_id, id);
CREATE INDEX reminder_log_item_idx ON reminder_log (item_id);

-- One fired reminder on one way of reaching the user, until that way confirms it.
CREATE TABLE deliveries (
    id uuid PRIMARY KEY,
    reminder_id uuid NOT NULL REFERENCES reminder_log ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    item_id uuid NOT NULL REFERENCES items ON DELETE CASCADE,
    connection_id uuid REFERENCES connections ON DELETE CASCADE,
    token_id uuid REFERENCES personal_tokens ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed', 'superseded', 'cancelled', 'expired')),
    reason text NOT NULL DEFAULT '',
    attempts integer NOT NULL DEFAULT 0,
    held_until timestamptz,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    closed_at timestamptz,
    CHECK ((connection_id IS NULL) <> (token_id IS NULL))
);
CREATE INDEX deliveries_pending_idx ON deliveries (expires_at) WHERE status = 'pending';
CREATE INDEX deliveries_connection_idx ON deliveries (connection_id) WHERE status = 'pending';
CREATE INDEX deliveries_token_idx ON deliveries (token_id) WHERE status = 'pending';
CREATE INDEX deliveries_reminder_idx ON deliveries (reminder_id);
CREATE INDEX deliveries_item_idx ON deliveries (item_id);

-- +goose Down
DROP TABLE deliveries;
DROP TABLE reminder_log;
DROP TABLE scheduled_reminders;
ALTER TABLE personal_tokens DROP COLUMN remind;
ALTER TABLE connection_grants DROP COLUMN remind;
DELETE FROM connection_grants WHERE enabled IS NULL;
ALTER TABLE connection_grants ALTER COLUMN enabled SET NOT NULL;
ALTER TABLE connections DROP COLUMN allowed_intents;
ALTER TABLE users DROP CONSTRAINT users_quiet_both;
UPDATE users SET quiet_start = '22:00', quiet_end = '08:00' WHERE quiet_start IS NULL;
ALTER TABLE users
    ALTER COLUMN quiet_start SET NOT NULL,
    ALTER COLUMN quiet_start SET DEFAULT '22:00',
    ALTER COLUMN quiet_end SET NOT NULL,
    ALTER COLUMN quiet_end SET DEFAULT '08:00';
