package config

import (
	"kronos-scheduler/handlers"
	"kronos-scheduler/routes"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func CreateServer() *fiber.App {
	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Initialize handlers with logger
	handlers.InitLogger(logger)

	// Create Fiber app
	app := fiber.New()

	routes.SetupRoutes(app)

	return app
}
