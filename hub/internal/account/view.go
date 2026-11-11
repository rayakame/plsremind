package account

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

type QuietHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type User struct {
	ID          uuid.UUID  `json:"id"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"`
	Timezone    string     `json:"timezone"`
	QuietHours  QuietHours `json:"quiet_hours"`
	CreatedAt   time.Time  `json:"created_at"`
}

func View(u sqlc.User) User {
	return User{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Timezone:    u.Timezone,
		QuietHours:  QuietHours{Start: formatClock(u.QuietStart), End: formatClock(u.QuietEnd)},
		CreatedAt:   u.CreatedAt.UTC(),
	}
}

func formatClock(t pgtype.Time) string {
	minutes := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func parseClock(s string) (pgtype.Time, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return pgtype.Time{}, false
	}
	micros := int64(t.Hour()*60+t.Minute()) * int64(time.Minute/time.Microsecond)
	return pgtype.Time{Microseconds: micros, Valid: true}, true
}
