package delivery

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func registerTests(root, user fiber.Router, d *core.Deps) {
	user.Post("/connections/:id/tests", auth.RequireSession(), startTest(d))
	user.Get("/connections/:id/tests/:test", auth.RequireSession(), getTest(d))
	root.Put("/deliveries/tests/:id", answerTest(d))
}

func startTest(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		access, err := d.Q().ConnectionTestAccess(c.Context(), sqlc.ConnectionTestAccessParams{UserID: userID, ConnectionID: id})
		if store.IsNotFound(err) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		if !access.Deliverable || !access.Linked || !access.Granted {
			return api.Conflict("test_unavailable", "This app cannot send you a test: it does not remind, may not act for you, or has no account of yours to write to.")
		}
		// Each test writes to the user through the app; a held-down button must not flood them.
		if _, reset, ok := d.RateTest.Allow(userID.String() + ":" + id.String()); !ok {
			wait := int(reset.Sub(d.Now()).Round(time.Second) / time.Second)
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(max(wait, 1)))
			return api.NewError(http.StatusTooManyRequests, "rate_limited", "Wait a moment before testing this app again.")
		}
		var row sqlc.ConnectionTest
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err = q.CreateConnectionTest(c.Context(), sqlc.CreateConnectionTestParams{
				ID:           uuid.Must(uuid.NewV7()),
				UserID:       userID,
				ConnectionID: id,
				CreatedAt:    d.Now(),
			})
			if err != nil {
				return err
			}
			return events.SendSignal(c.Context(), q, events.Signal{Type: events.SignalTest, TestID: &row.ID})
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(viewTest(row))
	}
}

func getTest(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		testID, err := api.ParamUUID(c, "test")
		if err != nil {
			return err
		}
		row, err := d.Q().GetUserConnectionTest(c.Context(), sqlc.GetUserConnectionTestParams{ID: testID, UserID: auth.Target(c), ConnectionID: id})
		if store.IsNotFound(err) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		return c.JSON(viewTest(row))
	}
}

func answerTest(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		ch, err := channel(c, d)
		if err != nil {
			return err
		}
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		status, reason, err := outcome(c)
		if err != nil {
			return err
		}
		var out sqlc.ConnectionTest
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err := q.LockConnectionTest(c.Context(), id)
			// A test of another app does not exist as far as this one is concerned.
			if store.IsNotFound(err) || (err == nil && (ch.ConnectionID == nil || *ch.ConnectionID != row.ConnectionID)) {
				return api.NotFound()
			}
			if err != nil {
				return err
			}
			now := d.Now()
			// Past its minute the test is over, even before the ticker has marked it so.
			if row.Status == "pending" && !row.CreatedAt.After(now.Add(-testAnswerWithin)) {
				return api.Conflict("test_closed", "The test is already unanswered.")
			}
			switch row.Status {
			case "pending":
				out, err = q.CloseConnectionTest(c.Context(), sqlc.CloseConnectionTestParams{ID: id, Status: status, Reason: reason, Now: &now})
				return err
			case status:
				out = row
				return nil
			}
			return api.Conflict("test_closed", "The test is already "+row.Status+".")
		})
		if err != nil {
			return err
		}
		return c.JSON(viewTest(out))
	}
}
