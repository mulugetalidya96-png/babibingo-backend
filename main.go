package main

import (
	"babibingo/internal/api"
	"babibingo/internal/bot"
	"babibingo/internal/config"
	"babibingo/internal/game"
	"babibingo/internal/repository"
	"context"
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	cfg := config.Load()

	if err := game.LoadCardsFromJSON("data/cards.json"); err != nil {
    log.Fatalf("Failed to load cards: %v", err)
}

	// Initialize database
	db, err := repository.InitDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Initialize Redis
	rdb := repository.InitRedis(cfg.RedisURL)

	// Initialize game engine
	engine := game.NewEngine(db, rdb)
	go engine.Run()

	// Initialize Telegram bot
	// Initialize Telegram bot
if cfg.BotToken != "" {

	telegramBot, err := bot.New(
		cfg.BotToken,
		cfg.WebAppURL,
		db,
		rdb,
		cfg,
	)

	if err != nil {
		log.Fatalf(
			"Failed to initialize telegram bot: %v",
			err,
		)
	}


	go func() {

		if err := telegramBot.Start(
			context.Background(),
		); err != nil {

			log.Printf(
				"Telegram bot stopped: %v",
				err,
			)
		}

	}()

}

	// Setup HTTP server
	router := gin.Default()

	// CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Telegram-Init-Data"},
		AllowCredentials: true,
	}))

	// Register routes
	api.RegisterRoutes(router, db, rdb, engine)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
