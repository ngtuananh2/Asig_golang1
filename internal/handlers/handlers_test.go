package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"taskapi/internal/models"
	"taskapi/internal/repositories"
	"taskapi/internal/services"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Project{}, &models.Task{}, &models.Comment{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return New(db, services.New(repositories.New(db)), "test-secret-123456")
}

func TestRegisterAndLoginReturnsToken(t *testing.T) {
	r := gin.New()
	h := testHandler(t)
	h.Register(r)

	registerBody := `{"name":"Ada Lovelace","email":"ada@example.com","password":"correct-horse"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(registerBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var reg struct {
		Data struct {
			ID    uint   `json:"id"`
			Email string `json:"email"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reg))
	assert.NotZero(t, reg.Data.ID)
	assert.Equal(t, "ada@example.com", reg.Data.Email)

	loginBody := `{"email":"ada@example.com","password":"correct-horse"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var login struct {
		Data struct {
			Token string `json:"token"`
			User  struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	assert.NotEmpty(t, login.Data.Token)
	assert.Equal(t, "ada@example.com", login.Data.User.Email)
}

func TestListProjectsRequiresAuth(t *testing.T) {
	r := gin.New()
	h := testHandler(t)
	h.Register(r)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateProjectStoresOwnedProject(t *testing.T) {
	r := gin.New()
	h := testHandler(t)
	h.Register(r)

	registerBody := `{"name":"Ada Lovelace","email":"ada@example.com","password":"correct-horse"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(registerBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	loginBody := `{"email":"ada@example.com","password":"correct-horse"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	require.NotEmpty(t, login.Data.Token)

	projectBody := `{"name":"Research roadmap","description":"Plan the upcoming sprint"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(projectBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+login.Data.Token)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var project struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &project))
	assert.Equal(t, "Research roadmap", project.Data.Name)
}
