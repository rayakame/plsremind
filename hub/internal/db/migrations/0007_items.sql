-- +goose Up
CREATE TABLE items (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    list_id uuid NOT NULL REFERENCES lists,
    title text NOT NULL,
    note text NOT NULL DEFAULT '',
    priority text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high')),
    due_at timestamptz,
    due_date date,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done')),
    completed_at timestamptz,
    reminder_override jsonb,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (due_at IS NULL OR due_date IS NULL)
);
CREATE INDEX items_user_status_idx ON items (user_id, status);
CREATE INDEX items_list_id_idx ON items (list_id);

-- +goose StatementBegin
-- An all-day deadline is due at the last instant that still falls on its date in the
-- owner's time zone. Where the clocks are set back at midnight, 00:00 of the next day
-- happens twice and AT TIME ZONE picks the later one, so the first branch steps back
-- to the earlier one.
CREATE FUNCTION item_due(due_at timestamptz, due_date date, tz text) RETURNS timestamptz
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(due_at, (
        SELECT CASE
            WHEN (c.t AT TIME ZONE tz)::date > due_date
                THEN c.t - ((c.t AT TIME ZONE tz) - (due_date + 1)::timestamp) - interval '1 second'
            ELSE c.t
        END
        FROM (SELECT ((due_date + 1)::timestamp AT TIME ZONE tz) - interval '1 second' AS t) c
    ))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION item_due(timestamptz, date, text);
DROP TABLE items;
