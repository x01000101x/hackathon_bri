package models

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user model in the system
type User struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Email     string         `gorm:"uniqueIndex;type:varchar(255);not null" json:"email"`
	Password  string         `gorm:"type:varchar(255);not null" json:"-"`
	Name      string         `gorm:"type:varchar(100)" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	IsEod     string         `gorm:"type:varchar(10);default:null" json:"is_eod"`
	EodReason string         `gorm:"type:text;default:null" json:"eod_reason"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate is a GORM hook that runs before a record is created
func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	// Let's assume we validate email or encrypt password here
	if u.Email == "" {
		return gorm.ErrRecordNotFound // Dummy error for validation
	}
	return nil
}

// FindActiveUsers performs a GORM query to fetch active users
func FindActiveUsers(db *gorm.DB) ([]User, error) {
	var users []User
	// Potentially inefficient query or unindexed search for testing
	err := db.Where("is_active = ?", true).Order("created_at DESC").Find(&users).Error
	return users, err
}
