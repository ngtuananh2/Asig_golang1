package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"taskapi/internal/handlers"
	"taskapi/internal/middlewares"
	"taskapi/internal/models"
	"taskapi/internal/repositories"
	"taskapi/internal/services"
)

func main() {
	dsn := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/taskapi?sslmode=disable")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("database connection: ", err)
	}
	if err = db.AutoMigrate(&models.User{}, &models.Project{}, &models.Task{}, &models.Comment{}); err != nil {
		log.Fatal("database migration: ", err)
	}
	secret := env("JWT_SECRET", "local-development-secret-change-me")
	if len(secret) < 16 {
		log.Fatal("JWT_SECRET must contain at least 16 characters")
	}
	r := gin.New()
	r.Use(gin.Logger(), middlewares.Recovery(), cors.New(cors.Config{AllowAllOrigins: true, AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}, AllowHeaders: []string{"Authorization", "Content-Type"}}), middlewares.RateLimit(120, time.Minute))
	service := services.New(repositories.New(db))
	h := handlers.New(db, service, secret)
	if addr := env("REDIS_URL", ""); addr != "" {
		var client *redis.Client
		if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
			options, parseErr := redis.ParseURL(addr)
			if parseErr != nil {
				log.Fatal("invalid REDIS_URL: ", parseErr)
			}
			client = redis.NewClient(options)
		} else {
			client = redis.NewClient(&redis.Options{Addr: addr})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := client.Ping(ctx).Err(); err != nil {
			log.Printf("Redis unavailable; continuing without cache: %v", err)
			_ = client.Close()
		} else {
			h.Cache = client
		}
		cancel()
	}
	h.Register(r)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "route not found"}})
	})
	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := srv.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Fatal(e)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = srv.Shutdown(ctx); err != nil {
		log.Fatal("shutdown: ", err)
	}
	if h.Cache != nil {
		_ = h.Cache.Close()
	}
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
