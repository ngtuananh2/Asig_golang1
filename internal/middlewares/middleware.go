package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

func Auth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		token, err := jwt.Parse(h, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, http.ErrNotSupported
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"code": "unauthorized", "message": "missing or invalid bearer token"}})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"code": "unauthorized", "message": "invalid token claims"}})
			return
		}
		sub, valid := claims["sub"].(string)
		userID, parseErr := strconv.ParseUint(sub, 10, 32)
		if !valid || parseErr != nil || userID == 0 || claims["exp"] == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"code": "unauthorized", "message": "invalid token subject"}})
			return
		}
		c.Set("user_id", uint(userID))
		c.Next()
	}
}

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "internal_error", "message": "request could not be completed"}})
			}
		}()
		c.Next()
	}
}

// Cache caches successful GET responses per authenticated user. Redis errors fail open.
func Cache(client *redis.Client, ttl time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if client == nil || c.Request.Method != http.MethodGet {
			c.Next()
			return
		}
		uid := c.GetUint("user_id")
		key := fmt.Sprintf("taskapi:cache:%d:%s", uid, c.Request.URL.RequestURI())
		cached, err := client.Get(c.Request.Context(), key).Bytes()
		if err == nil {
			c.Data(http.StatusOK, "application/json; charset=utf-8", cached)
			c.Abort()
			return
		}
		w := &cacheWriter{ResponseWriter: c.Writer}
		c.Writer = w
		c.Next()
		if c.Writer.Status() == http.StatusOK {
			_ = client.Set(context.Background(), key, w.body.String(), ttl).Err()
		}
	}
}

type cacheWriter struct {
	gin.ResponseWriter
	body strings.Builder
}

func (w *cacheWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}
func (w *cacheWriter) WriteString(data string) (int, error) {
	w.body.WriteString(data)
	return w.ResponseWriter.WriteString(data)
}

// Invalidate removes cached responses for the current user after a mutation.
func Invalidate(client *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if client == nil || c.Request.Method == http.MethodGet || c.Writer.Status() >= 400 {
			return
		}
		uid := strconv.FormatUint(uint64(c.GetUint("user_id")), 10)
		ctx := context.Background()
		iter := client.Scan(ctx, 0, "taskapi:cache:"+uid+":*", 100).Iterator()
		keys := make([]string, 0)
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if len(keys) > 0 {
			_ = client.Del(ctx, keys...).Err()
		}
	}
}

func RateLimit(max int, window time.Duration) gin.HandlerFunc {
	type entry struct {
		count int
		until time.Time
	}
	var mu sync.Mutex
	clients := map[string]entry{}
	return func(c *gin.Context) {
		mu.Lock()
		now := time.Now()
		k := c.ClientIP()
		e := clients[k]
		if now.After(e.until) {
			e = entry{until: now.Add(window)}
		}
		e.count++
		clients[k] = e
		mu.Unlock()
		if e.count > max {
			c.AbortWithStatusJSON(429, gin.H{"error": gin.H{"code": "rate_limited", "message": "too many requests"}})
			return
		}
		c.Next()
	}
}
