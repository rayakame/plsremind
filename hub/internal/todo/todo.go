// Package todo holds what a user keeps track of: lists, todos and deadlines with
// their subitems and manual reminders. A deadline is an item with a due time.
package todo

import (
	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/core"
)

func Register(user fiber.Router, d *core.Deps) {
	registerLists(user, d)
	registerItems(user, d)
	registerSubitems(user, d)
	registerReminders(user, d)
}
