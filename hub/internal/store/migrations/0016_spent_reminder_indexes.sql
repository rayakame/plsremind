-- +goose Up
CREATE INDEX manual_reminders_remind_at_idx ON manual_reminders (remind_at);
CREATE INDEX scheduled_reminders_manual_id_idx ON scheduled_reminders (manual_id) WHERE manual_id IS NOT NULL;

-- +goose Down
DROP INDEX scheduled_reminders_manual_id_idx;
DROP INDEX manual_reminders_remind_at_idx;
