package apierr

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func Validation(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "The request is invalid.", Fields: fields}
}

// Field is shorthand for a validation error on a single field.
func Field(name, msg string) *Error {
	return Validation(map[string]string{name: msg})
}

func NotFound() *Error {
	return New(http.StatusNotFound, "not_found", "The resource does not exist.")
}

func Unauthorized() *Error {
	return New(http.StatusUnauthorized, "unauthorized", "Authentication is required.")
}

func Forbidden() *Error {
	return New(http.StatusForbidden, "forbidden", "You are not allowed to do this.")
}

func Conflict(code, msg string) *Error {
	return New(http.StatusConflict, code, msg)
}

var fiberCodes = map[int]string{
	http.StatusBadRequest:            "bad_request",
	http.StatusNotFound:              "not_found",
	http.StatusMethodNotAllowed:      "method_not_allowed",
	http.StatusRequestEntityTooLarge: "payload_too_large",
}

// Handler renders every error in the API's error format.
func Handler(c fiber.Ctx, err error) error {
	var apiErr *Error
	var fiberErr *fiber.Error
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &fiberErr):
		code, ok := fiberCodes[fiberErr.Code]
		if !ok {
			code = "error"
		}
		apiErr = New(fiberErr.Code, code, fiberErr.Message)
	default:
		slog.Error("unhandled error", "method", c.Method(), "path", c.Path(), "err", err)
		apiErr = New(http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	return c.Status(apiErr.Status).JSON(fiber.Map{"error": apiErr})
}
