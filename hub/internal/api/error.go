// Package api holds what all HTTP handlers share: errors, request parsing, paging,
// rate limits and the duration format ("3d", "1d12h", "90m").
package api

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

func NewError(status int, code, msg string) *Error {
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
	return NewError(http.StatusNotFound, "not_found", "The resource does not exist.")
}

func Unauthorized() *Error {
	return NewError(http.StatusUnauthorized, "unauthorized", "Authentication is required.")
}

func Forbidden() *Error {
	return NewError(http.StatusForbidden, "forbidden", "You are not allowed to do this.")
}

func Conflict(code, msg string) *Error {
	return NewError(http.StatusConflict, code, msg)
}

var fiberCodes = map[int]string{
	http.StatusBadRequest:            "bad_request",
	http.StatusNotFound:              "not_found",
	http.StatusMethodNotAllowed:      "method_not_allowed",
	http.StatusRequestEntityTooLarge: "payload_too_large",
}

// ErrorHandler renders every error in the API's error format.
func ErrorHandler(c fiber.Ctx, err error) error {
	var apiErr *Error
	var fiberErr *fiber.Error
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &fiberErr):
		code, ok := fiberCodes[fiberErr.Code]
		if !ok {
			code = "error"
		}
		apiErr = NewError(fiberErr.Code, code, fiberErr.Message)
	default:
		slog.Error("unhandled error", "method", c.Method(), "path", c.Path(), "err", err)
		apiErr = NewError(http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	return c.Status(apiErr.Status).JSON(fiber.Map{"error": apiErr})
}
