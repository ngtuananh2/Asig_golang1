package services

import (
	"testing"

	"taskapi/internal/models"
	"taskapi/internal/repositories"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return New(repositories.New(db))
}

func TestRegisterStoresBcryptPassword(t *testing.T) {
	s := testService(t)
	u, err := s.Register("Ada Lovelace", "ada@example.com", "correct-horse")
	require.NoError(t, err)
	assert.NotEqual(t, "correct-horse", u.Password)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("correct-horse")))
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	s := testService(t)
	_, err := s.Register("Ada Lovelace", "ada@example.com", "correct-horse")
	require.NoError(t, err)
	_, err = s.Register("Another Ada", "ada@example.com", "correct-horse")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}

func TestAuthenticateAcceptsCorrectPasswordOnly(t *testing.T) {
	s := testService(t)
	_, err := s.Register("Ada Lovelace", "ada@example.com", "correct-horse")
	require.NoError(t, err)

	u, err := s.Authenticate("ada@example.com", "correct-horse")
	require.NoError(t, err)
	assert.Equal(t, "Ada Lovelace", u.Name)

	_, err = s.Authenticate("ada@example.com", "wrong-password")
	assert.Error(t, err)
}
