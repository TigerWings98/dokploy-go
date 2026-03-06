package main

import (
	"log"
	"os"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/config"
	"github.com/dokploy/dokploy/internal/db"
	"github.com/dokploy/dokploy/internal/handler"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	// Initialize auth
	a := auth.New(database)

	// Initialize Echo
	e := echo.New()
	e.HideBanner = true

	// Global middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: true,
	}))

	// Register all routes
	h := handler.New(database, a)
	h.RegisterRoutes(e)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Printf("Dokploy Go server starting on :%s", port)
	log.Printf("Base path: %s", cfg.Paths.BasePath)
	if err := e.Start(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
