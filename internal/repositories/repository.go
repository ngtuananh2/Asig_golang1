package repositories

import (
	"gorm.io/gorm"
)

// Repository provides the shared database boundary used by services.
type Repository struct{ DB *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{DB: db} }
