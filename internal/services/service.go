package services

import (
	"errors"
	"taskapi/internal/models"
	"taskapi/internal/repositories"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Service struct{ Repo *repositories.Repository }

func New(r *repositories.Repository) *Service { return &Service{Repo: r} }
func (s *Service) Register(name, email, password string) (*models.User, error) {
	var existing models.User
	if err := s.Repo.DB.Where("email = ?", email).First(&existing).Error; err == nil {
		return nil, errors.New("email already registered")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := models.User{Name: name, Email: email, Password: string(hash), Role: "user"}
	if err = s.Repo.DB.Create(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Service) Authenticate(email, password string) (*models.User, error) {
	var u models.User
	if err := s.Repo.DB.Where("email = ?", email).First(&u).Error; err != nil {
		return nil, errors.New("invalid email or password")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) != nil {
		return nil, errors.New("invalid email or password")
	}
	return &u, nil
}

var ErrNotFound = gorm.ErrRecordNotFound
