package models

import (
	"time"
)

type User struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
	Name          string    `gorm:"default:New User" json:"name"`
	Email         string    `gorm:"unique" json:"email"`
	Password      string    `json:"password"`
	IsVerified    bool      `gorm:"default:false" json:"is_verified"`
	Code          string    `json:"code"`
	CodeCreatedAt time.Time `gorm:"autoCreateTime" json:"codeCreatedAt"`
	PhoneNumber   string    `gorm:"unique;type:varchar(11);not null" json:"phoneNumber"`
	Avatar        string    `gorm:"default:'https://res.cloudinary.com/dqipg0or3/image/upload/v1728746922/uploads/oigc5k6e91shemck15uz.jpg'" json:"avatar"`
	Role          int       `gorm:"default:0" json:"role"`                   // 1: SuperAdmin - 2: Admin - 3: Receptionist - 0: User
	Status        int       `gorm:"default:0" json:"status"`                 // 0: active - 1: ban
	Gender        int       `json:"gender"`                                  // 0: Male, 1: Female, 2: Other
	DateOfBirth   string    `gorm:"default:'01/01/2000'" json:"dateOfBirth"` // Ngày sinh (format string, có thể là "YYYY-MM-DD")
	Banks         []Bank    `json:"banks" gorm:"foreignKey:UserId"`
	Children      []User    `gorm:"foreignKey:AdminId" json:"children,omitempty"`
	AdminId       *uint     `json:"adminId,omitempty"`
}
