package httpx

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
)

const (
	defaultLimit = 50
	maxLimit     = 100
)

type Page struct {
	Limit  int
	Cursor string
}

// ParsePage reads the limit and cursor query parameters.
func ParsePage(c fiber.Ctx) (Page, error) {
	page := Page{Limit: defaultLimit, Cursor: c.Query("cursor")}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			return Page{}, apierr.Field("limit", "Must be between 1 and 100.")
		}
		page.Limit = n
	}
	return page, nil
}

func errCursor() error {
	return apierr.New(http.StatusBadRequest, "invalid_cursor", "The cursor is not valid for this request.")
}

func EncodeCursor(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeCursor(s string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return errCursor()
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errCursor()
	}
	return nil
}

type List[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// NewList builds a page from limit+1 fetched rows; the extra row only signals that more exist.
func NewList[T any](rows []T, limit int, cursor func(last T) any) List[T] {
	list := List[T]{Data: rows}
	if len(rows) > limit {
		list.Data = rows[:limit]
		next := EncodeCursor(cursor(list.Data[limit-1]))
		list.NextCursor = &next
	}
	if list.Data == nil {
		list.Data = []T{}
	}
	return list
}
