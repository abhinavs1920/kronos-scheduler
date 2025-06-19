package main

import (
	"kronos-scheduler/config"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

)

const (
	Version = "v1.0.0" // Update this with each new version
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			// Handle sync error
		}
	}()

	sugar := logger.Sugar()
	sugar.Infof("Starting Kronos Scheduler %s", Version)

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		sugar.Info("No .env file found, using environment variables")
	}

	// Start version logger
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			sugar.Infof("Kronos Scheduler %s is running", Version)
		}
	}()

	// Create and start server
	app := config.CreateServer()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	sugar.Infof("Server starting on port %s", port)
	if err := app.Listen("0.0.0.0:" + port); err != nil {
		sugar.Fatalf("Failed to start server: %v", err)
	}
}
