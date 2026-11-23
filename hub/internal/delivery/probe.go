package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const (
	// testAnswerWithin is how long an app has to answer a test.
	testAnswerWithin = time.Minute
	// TestRetention is how long tests are kept for the user to see.
	TestRetention = 30 * 24 * time.Hour
)

// TestView is what an app receives with delivery.test: whom to write to, nothing else.
type TestView struct {
	ID   uuid.UUID `json:"id"`
	User User      `json:"user"`
}

// Test is a test of an app, as the user and the app see it.
type Test struct {
	ID           uuid.UUID  `json:"id"`
	ConnectionID uuid.UUID  `json:"connection_id"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
	CreatedAt    time.Time  `json:"created_at"`
	ClosedAt     *time.Time `json:"closed_at"`
}

func viewTest(row sqlc.ConnectionTest) Test {
	out := Test{
		ID:           row.ID,
		ConnectionID: row.ConnectionID,
		Status:       row.Status,
		Reason:       row.Reason,
		CreatedAt:    row.CreatedAt.UTC(),
	}
	if row.ClosedAt != nil {
		closed := row.ClosedAt.UTC()
		out.ClosedAt = &closed
	}
	return out
}

// TestFor loads a test that is still to be sent, with what its app needs to send it. One whose
// minute is over, or whose user has since been disabled or switched the app off, is not sent.
func TestFor(ctx context.Context, q *sqlc.Queries, now time.Time, id uuid.UUID) (view TestView, connection uuid.UUID, pending bool, err error) {
	row, err := q.GetConnectionTest(ctx, id)
	if store.IsNotFound(err) || (err == nil && (row.Status != "pending" || row.SentAt != nil || !row.CreatedAt.After(now.Add(-testAnswerWithin)))) {
		return TestView{}, uuid.Nil, false, nil
	}
	if err != nil {
		return TestView{}, uuid.Nil, false, err
	}
	access, err := q.ConnectionTestAccess(ctx, sqlc.ConnectionTestAccessParams{UserID: row.UserID, ConnectionID: row.ConnectionID})
	if store.IsNotFound(err) || (err == nil && (!access.Deliverable || !access.Linked || !access.Granted)) {
		return TestView{}, uuid.Nil, false, nil
	}
	if err != nil {
		return TestView{}, uuid.Nil, false, err
	}
	app, err := q.GetConnection(ctx, row.ConnectionID)
	if err != nil {
		return TestView{}, uuid.Nil, false, err
	}
	user, err := q.GetUser(ctx, row.UserID)
	if err != nil {
		return TestView{}, uuid.Nil, false, err
	}
	if user.Disabled {
		return TestView{}, uuid.Nil, false, nil
	}
	ids, err := q.ListExternalIDs(ctx, sqlc.ListExternalIDsParams{UserID: user.ID, Provider: app.Provider})
	if err != nil {
		return TestView{}, uuid.Nil, false, err
	}
	if ids == nil {
		ids = []string{}
	}
	view = TestView{
		ID:   row.ID,
		User: User{ID: user.ID, DisplayName: user.DisplayName, Timezone: user.Timezone, Locale: user.Locale, ExternalIDs: ids},
	}
	return view, row.ConnectionID, true, nil
}

// CloseTest ends a test that is still pending; one that already ended stays as it is.
func CloseTest(ctx context.Context, q *sqlc.Queries, now time.Time, id uuid.UUID, status, reason string) error {
	_, err := q.CloseConnectionTest(ctx, sqlc.CloseConnectionTestParams{ID: id, Status: status, Reason: reason, Now: &now})
	if store.IsNotFound(err) {
		return nil
	}
	return err
}

// ClaimTest marks a pending test as handed out and reports whether this call was the one that did.
func ClaimTest(ctx context.Context, q *sqlc.Queries, now time.Time, id uuid.UUID) (bool, error) {
	n, err := q.ClaimConnectionTest(ctx, sqlc.ClaimConnectionTestParams{ID: id, Now: &now})
	return n == 1, err
}

// UnsentTests lists the tests of a connection that were never handed out and can still be answered.
func UnsentTests(ctx context.Context, q *sqlc.Queries, now time.Time, connection uuid.UUID) ([]uuid.UUID, error) {
	return q.ListUnsentConnectionTests(ctx, sqlc.ListUnsentConnectionTestsParams{
		ConnectionID: connection,
		Since:        now.Add(-testAnswerWithin),
	})
}

func expireTests(ctx context.Context, q *sqlc.Queries, now time.Time) error {
	_, err := q.ExpireConnectionTests(ctx, sqlc.ExpireConnectionTestsParams{Now: &now, AnswerBy: now.Add(-testAnswerWithin)})
	return err
}
