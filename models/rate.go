package models

import "time"

type Rate struct {
	ID              uint      `json:"id" gorm:"primaryKey"` // ID đánh giá
	UserID          uint      `json:"userId"`               // Người dùng thực hiện đánh giá
	AccommodationID uint      `json:"accommodationId"`      // Chỗ ở được đánh giá (liên kết với bảng Accommodation)
	Comment         string    `json:"comment"`              // Bình luận của người dùng
	Star            int       `json:"star"`                 // Số sao (điểm đánh giá)
	CreateAt        time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdateAt        time.Time `gorm:"autoUpdateTime" json:"updatedAt"` // Ngày cập nhật đánh giá
	User            User      `json:"user" gorm:"foreignKey:UserID"`   // Người dùng đánh giá
}
