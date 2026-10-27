package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"taskapi/internal/middlewares"
	"taskapi/internal/models"
	"taskapi/internal/services"
)

type Handler struct {
	DB      *gorm.DB
	Service *services.Service
	Secret  string
	Cache   *redis.Client
}

func New(db *gorm.DB, s *services.Service, secret string) *Handler {
	return &Handler{DB: db, Service: s, Secret: secret}
}
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"data": gin.H{"status": "ok"}}) })
	v := r.Group("/api/v1")
	v.POST("/auth/register", h.register)
	v.POST("/auth/login", h.login)
	p := v.Group("/projects")
	p.Use(middlewares.Auth(h.Secret), middlewares.Cache(h.Cache, 30*time.Second), middlewares.Invalidate(h.Cache))
	p.GET("", h.list("projects"))
	p.POST("", h.create("projects"))
	p.GET("/:id", h.get("projects"))
	p.PUT("/:id", h.update("projects"))
	p.DELETE("/:id", h.delete("projects"))
	t := v.Group("/tasks")
	t.Use(middlewares.Auth(h.Secret), middlewares.Cache(h.Cache, 30*time.Second), middlewares.Invalidate(h.Cache))
	t.GET("", h.list("tasks"))
	t.POST("", h.create("tasks"))
	t.GET("/:id", h.get("tasks"))
	t.PUT("/:id", h.update("tasks"))
	t.DELETE("/:id", h.delete("tasks"))
	c := v.Group("/comments")
	c.Use(middlewares.Auth(h.Secret), middlewares.Cache(h.Cache, 30*time.Second), middlewares.Invalidate(h.Cache))
	c.GET("", h.list("comments"))
	c.POST("", h.create("comments"))
	c.GET("/:id", h.get("comments"))
	c.PUT("/:id", h.update("comments"))
	c.DELETE("/:id", h.delete("comments"))
	u := v.Group("/users")
	u.Use(middlewares.Auth(h.Secret), middlewares.Cache(h.Cache, 30*time.Second), middlewares.Invalidate(h.Cache))
	u.GET("", h.listUsers)
	u.POST("", h.createUser)
	u.GET("/:id", h.getUser)
	u.PUT("/:id", h.updateUser)
	u.DELETE("/:id", h.deleteUser)
}

func (h *Handler) adminOrSelf(c *gin.Context, id uint) bool {
	if c.GetUint("user_id") == id {
		return true
	}
	var u models.User
	return h.DB.Select("role").First(&u, c.GetUint("user_id")).Error == nil && u.Role == "admin"
}
func (h *Handler) listUsers(c *gin.Context) {
	var me models.User
	if h.DB.Select("role").First(&me, c.GetUint("user_id")).Error != nil || me.Role != "admin" {
		fail(c, 403, "forbidden", "admin role required")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var users []models.User
	var total int64
	q := h.DB.Model(&models.User{})
	if s := c.Query("q"); s != "" {
		q = q.Where("name ILIKE ? OR email ILIKE ?", "%"+s+"%", "%"+s+"%")
	}
	sortBy, direction := listOrder(c, "users")
	if err := q.Count(&total).Error; err != nil {
		internal(c)
		return
	}
	if err := q.Order(sortBy + " " + direction).Offset((page - 1) * limit).Limit(limit).Find(&users).Error; err != nil {
		internal(c)
		return
	}
	c.JSON(200, gin.H{"data": users, "meta": gin.H{"page": page, "limit": limit, "total": total}})
}
func (h *Handler) createUser(c *gin.Context) {
	var me models.User
	if h.DB.Select("role").First(&me, c.GetUint("user_id")).Error != nil || me.Role != "admin" {
		fail(c, 403, "forbidden", "admin role required")
		return
	}
	var in struct {
		Name     string `json:"name" binding:"required,min=2,max=120"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "validation_error", "name, valid email and password (8+ chars) are required")
		return
	}
	u, err := h.Service.Register(strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Email)), in.Password)
	if err != nil {
		fail(c, 409, "registration_failed", "email may already be registered")
		return
	}
	c.JSON(201, gin.H{"data": u})
}
func (h *Handler) getUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		fail(c, 400, "invalid_id", "id must be a positive integer")
		return
	}
	if !h.adminOrSelf(c, uint(id)) {
		fail(c, 403, "forbidden", "cannot access this user")
		return
	}
	var u models.User
	if h.DB.First(&u, uint(id)).Error != nil {
		fail(c, 404, "not_found", "user not found")
		return
	}
	c.JSON(200, gin.H{"data": u})
}
func (h *Handler) updateUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		fail(c, 400, "invalid_id", "id must be a positive integer")
		return
	}
	if !h.adminOrSelf(c, uint(id)) {
		fail(c, 403, "forbidden", "cannot update this user")
		return
	}
	var in struct {
		Name     *string `json:"name" binding:"omitempty,min=2,max=120"`
		Email    *string `json:"email" binding:"omitempty,email"`
		Password *string `json:"password" binding:"omitempty,min=8"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "validation_error", "invalid user fields")
		return
	}
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil {
		updates["email"] = strings.ToLower(strings.TrimSpace(*in.Email))
	}
	if in.Password != nil { // Reuse bcrypt policy from registration without exposing hashes.
		hash, e := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if e != nil {
			internal(c)
			return
		}
		updates["password"] = string(hash)
	}
	if len(updates) == 0 {
		fail(c, 400, "validation_error", "at least one field is required")
		return
	}
	if e := h.DB.Model(&models.User{}).Where("id = ?", uint(id)).Updates(updates).Error; e != nil {
		fail(c, 409, "update_failed", "email may already be in use")
		return
	}
	h.getUser(c)
}
func (h *Handler) deleteUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		fail(c, 400, "invalid_id", "id must be a positive integer")
		return
	}
	if !h.adminOrSelf(c, uint(id)) {
		fail(c, 403, "forbidden", "cannot delete this user")
		return
	}
	if uint(id) == c.GetUint("user_id") {
		fail(c, 400, "invalid_operation", "cannot delete the active account")
		return
	}
	res := h.DB.Delete(&models.User{}, uint(id))
	if res.Error != nil {
		internal(c)
		return
	}
	if res.RowsAffected == 0 {
		fail(c, 404, "not_found", "user not found")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) register(c *gin.Context) {
	var in struct {
		Name     string `json:"name" binding:"required,min=2,max=120"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "validation_error", "name, valid email and password (8+ chars) are required")
		return
	}
	u, e := h.Service.Register(strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Email)), in.Password)
	if e != nil {
		fail(c, 409, "registration_failed", e.Error())
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"id": u.ID, "name": u.Name, "email": u.Email}})
}
func (h *Handler) login(c *gin.Context) {
	var in struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "validation_error", "valid email and password are required")
		return
	}
	u, e := h.Service.Authenticate(strings.ToLower(strings.TrimSpace(in.Email)), in.Password)
	if e != nil {
		fail(c, 401, "unauthorized", "invalid email or password")
		return
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": strconv.FormatUint(uint64(u.ID), 10), "exp": time.Now().Add(24 * time.Hour).Unix()}).SignedString([]byte(h.Secret))
	c.JSON(200, gin.H{"data": gin.H{"token": tok, "user": gin.H{"id": u.ID, "name": u.Name, "email": u.Email, "role": u.Role}}})
}
func (h *Handler) list(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetUint("user_id")
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		var rows any
		var total int64
		var q *gorm.DB
		sortBy, direction := listOrder(c, kind)
		switch kind {
		case "projects":
			q = h.DB.Model(&models.Project{}).Where("user_id = ?", uid)
			if s := c.Query("q"); s != "" {
				q = q.Where("name ILIKE ?", "%"+s+"%")
			}
			var v []models.Project
			if err := q.Count(&total).Error; err != nil {
				internal(c)
				return
			}
			if err := q.Order(sortBy + " " + direction).Offset((page - 1) * limit).Limit(limit).Find(&v).Error; err != nil {
				internal(c)
				return
			}
			rows = v
		case "tasks":
			q = h.DB.Model(&models.Task{}).Where("user_id = ?", uid)
			if status := c.Query("status"); status != "" {
				q = q.Where("status = ?", status)
			}
			if projectID := c.Query("project_id"); projectID != "" {
				q = q.Where("project_id = ?", projectID)
			}
			if err := q.Count(&total).Error; err != nil {
				internal(c)
				return
			}
			var v []models.Task
			if err := q.Order(sortBy + " " + direction).Offset((page - 1) * limit).Limit(limit).Find(&v).Error; err != nil {
				internal(c)
				return
			}
			rows = v
		default:
			q = h.DB.Model(&models.Comment{}).Where("user_id = ?", uid)
			if taskID := c.Query("task_id"); taskID != "" {
				q = q.Where("task_id = ?", taskID)
			}
			if err := q.Count(&total).Error; err != nil {
				internal(c)
				return
			}
			var v []models.Comment
			if err := q.Order(sortBy + " " + direction).Offset((page - 1) * limit).Limit(limit).Find(&v).Error; err != nil {
				internal(c)
				return
			}
			rows = v
		}
		c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": page, "limit": limit, "total": total}})
	}
}

func listOrder(c *gin.Context, kind string) (string, string) {
	field := c.DefaultQuery("sort", "id")
	direction := strings.ToUpper(c.DefaultQuery("order", "desc"))
	if direction != "ASC" && direction != "DESC" {
		direction = "DESC"
	}
	allowed := map[string]map[string]bool{
		"projects": {"id": true, "name": true, "created_at": true},
		"tasks":    {"id": true, "title": true, "status": true, "due_at": true, "created_at": true},
		"comments": {"id": true, "created_at": true},
		"users":    {"id": true, "name": true, "email": true, "created_at": true},
	}[kind]
	if !allowed[field] {
		field = "id"
	}
	return field, direction
}
func validTaskStatus(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}
func (h *Handler) create(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetUint("user_id")
		switch kind {
		case "projects":
			var v models.Project
			if c.ShouldBindJSON(&v) != nil || strings.TrimSpace(v.Name) == "" {
				fail(c, 400, "validation_error", "name is required")
				return
			}
			v.ID = 0
			v.UserID = uid
			if e := h.DB.Create(&v).Error; e != nil {
				internal(c)
				return
			}
			c.JSON(201, gin.H{"data": v})
		case "tasks":
			var v models.Task
			if c.ShouldBindJSON(&v) != nil || strings.TrimSpace(v.Title) == "" || v.ProjectID == 0 {
				fail(c, 400, "validation_error", "project_id and title are required")
				return
			}
			var p models.Project
			if h.DB.Where("id = ? AND user_id = ?", v.ProjectID, uid).First(&p).Error != nil {
				fail(c, 404, "not_found", "project not found")
				return
			}
			v.UserID = uid
			if v.Status == "" {
				v.Status = "todo"
			}
			if !validTaskStatus(v.Status) {
				fail(c, 400, "validation_error", "status must be todo, in_progress, or done")
				return
			}
			v.ID = 0
			if e := h.DB.Create(&v).Error; e != nil {
				internal(c)
				return
			}
			c.JSON(201, gin.H{"data": v})
		default:
			var v models.Comment
			if c.ShouldBindJSON(&v) != nil || strings.TrimSpace(v.Body) == "" || v.TaskID == 0 {
				fail(c, 400, "validation_error", "task_id and body are required")
				return
			}
			var t models.Task
			if h.DB.Where("id = ? AND user_id = ?", v.TaskID, uid).First(&t).Error != nil {
				fail(c, 404, "not_found", "task not found")
				return
			}
			v.UserID = uid
			v.ID = 0
			if e := h.DB.Create(&v).Error; e != nil {
				internal(c)
				return
			}
			c.JSON(201, gin.H{"data": v})
		}
	}
}
func (h *Handler) get(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil || id < 1 {
			fail(c, 400, "invalid_id", "id must be a positive integer")
			return
		}
		uid := c.GetUint("user_id")
		var row any
		var err error
		switch kind {
		case "projects":
			v := new(models.Project)
			err = h.DB.Where("user_id = ?", uid).First(v, id).Error
			row = v
		case "tasks":
			v := new(models.Task)
			err = h.DB.Where("user_id = ?", uid).First(v, id).Error
			row = v
		default:
			v := new(models.Comment)
			err = h.DB.Where("user_id = ?", uid).First(v, id).Error
			row = v
		}
		if err != nil {
			fail(c, 404, "not_found", "resource not found")
			return
		}
		c.JSON(200, gin.H{"data": row})
	}
}
func (h *Handler) update(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil || id < 1 {
			fail(c, 400, "invalid_id", "invalid id")
			return
		}
		uid := c.GetUint("user_id")
		var body map[string]any
		if c.ShouldBindJSON(&body) != nil {
			fail(c, 400, "validation_error", "invalid JSON body")
			return
		}
		allowed := map[string]bool{}
		switch kind {
		case "projects":
			allowed = map[string]bool{"name": true, "description": true}
		case "tasks":
			allowed = map[string]bool{"title": true, "description": true, "status": true, "due_at": true}
		default:
			allowed = map[string]bool{"body": true}
		}
		for key := range body {
			if !allowed[key] {
				delete(body, key)
			}
		}
		if len(body) == 0 {
			fail(c, 400, "validation_error", "at least one field is required")
			return
		}
		for key, value := range body {
			if (key == "name" || key == "title" || key == "body") && strings.TrimSpace(value.(string)) == "" {
				fail(c, 400, "validation_error", key+" cannot be empty")
				return
			}
		}
		if status, ok := body["status"].(string); ok && !validTaskStatus(status) {
			fail(c, 400, "validation_error", "status must be todo, in_progress, or done")
			return
		}
		var model any
		switch kind {
		case "projects":
			model = &models.Project{}
		case "tasks":
			model = &models.Task{}
		default:
			model = &models.Comment{}
		}
		res := h.DB.Model(model).Where("id = ? AND user_id = ?", id, uid).Updates(body)
		if res.Error != nil {
			internal(c)
			return
		}
		if res.RowsAffected == 0 {
			fail(c, 404, "not_found", "resource not found")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"id": id, "updated": true}})
	}
}
func (h *Handler) delete(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil || id < 1 {
			fail(c, 400, "invalid_id", "invalid id")
			return
		}
		uid := c.GetUint("user_id")
		var model any
		switch kind {
		case "projects":
			model = &models.Project{}
		case "tasks":
			model = &models.Task{}
		default:
			model = &models.Comment{}
		}
		res := h.DB.Where("id = ? AND user_id = ?", id, uid).Delete(model)
		if res.Error != nil {
			internal(c)
			return
		}
		if res.RowsAffected == 0 {
			fail(c, 404, "not_found", "resource not found")
			return
		}
		c.Status(http.StatusNoContent)
	}
}
func fail(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
func internal(c *gin.Context) { fail(c, 500, "internal_error", "request could not be completed") }
