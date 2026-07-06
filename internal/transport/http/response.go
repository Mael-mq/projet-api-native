package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/mmarquet/native-api/internal/service"
)

// format d'erreur normalisé renvoyé par l'api
type apiError struct {
	Error errBody `json:"error"`
}

type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// mappe une erreur métier -> réponse http normalisée
func fail(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	tag := "INTERNAL_ERROR"
	switch {
	case errors.Is(err, service.ErrNotFound):
		code, tag = fiber.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, service.ErrConflict):
		code, tag = fiber.StatusConflict, "CONFLICT"
	}
	return c.Status(code).JSON(apiError{Error: errBody{Code: tag, Message: err.Error()}})
}

// erreur de validation d'entrée
func badRequest(c *fiber.Ctx, err error) error {
	return c.Status(fiber.StatusBadRequest).JSON(apiError{
		Error: errBody{Code: "VALIDATION_ERROR", Message: err.Error()},
	})
}
