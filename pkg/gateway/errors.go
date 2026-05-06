package gateway

import (
	"github.com/gofiber/fiber/v2"
)

func openAIError(code, message, errType string) fiber.Map {
	return fiber.Map{
		"error": fiber.Map{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	}
}
