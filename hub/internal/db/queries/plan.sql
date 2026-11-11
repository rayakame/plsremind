-- name: ListReminderPlans :many
SELECT * FROM reminder_plans WHERE user_id = @user_id;

-- name: UpsertReminderPlan :exec
INSERT INTO reminder_plans (user_id, priority, before_seconds, follow_up)
VALUES (@user_id, @priority, @before_seconds, @follow_up)
ON CONFLICT (user_id, priority)
DO UPDATE SET before_seconds = EXCLUDED.before_seconds, follow_up = EXCLUDED.follow_up;

-- name: DeleteReminderPlan :exec
DELETE FROM reminder_plans WHERE user_id = @user_id AND priority = @priority;
