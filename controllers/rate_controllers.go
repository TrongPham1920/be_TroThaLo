package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"new/config"
	"new/models"
	"new/services"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type RateResponse struct {
	ID              uint      `json:"id"`
	AccommodationID uint      `json:"accommodationId"`
	Comment         string    `json:"comment"`
	Star            int       `json:"star"`
	CreateAt        time.Time `json:"createAt"`
	UpdateAt        time.Time `json:"updateAt"`
	User            UserInfo  `json:"user"`
}

type RateUpdateResponse struct {
	ID              uint      `json:"id"`
	AccommodationID uint      `json:"accommodationId"`
	Comment         string    `json:"comment"`
	Star            int       `json:"star"`
	CreateAt        time.Time `json:"createAt"`
	UpdateAt        time.Time `json:"updateAt"`
}

type UserInfo struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

func GetAllRates(c *gin.Context) {
	accommodationIdFilter := c.DefaultQuery("accommodationId", "")

	cacheKey := "rates:all"
	if accommodationIdFilter != "" {
		cacheKey = fmt.Sprintf("rates:accommodation:%s", accommodationIdFilter)
	}

	// Kết nối Redis
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis", "error": err.Error()})
		return
	}

	var rates []models.Rate

	// Lấy dữ liệu từ Redis
	err = services.GetFromRedis(config.Ctx, rdb, cacheKey, &rates)
	if err == nil && len(rates) > 0 {
		var rateResponses []RateResponse
		for _, rate := range rates {
			rateResponse := RateResponse{
				ID:              rate.ID,
				AccommodationID: rate.AccommodationID,
				Comment:         rate.Comment,
				Star:            rate.Star,
				CreateAt:        rate.CreateAt,
				UpdateAt:        rate.UpdateAt,
				User: UserInfo{
					ID:     rate.User.ID,
					Name:   rate.User.Name,
					Avatar: rate.User.Avatar,
				},
			}
			rateResponses = append(rateResponses, rateResponse)
		}
		c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách đánh giá thành công từ cache", "data": rateResponses})
		return
	}

	// Lấy dữ liệu từ database
	tx := config.DB.Preload("User")
	if accommodationIdFilter != "" {
		if parsedAccommodationId, err := strconv.Atoi(accommodationIdFilter); err == nil {
			tx = tx.Where("accommodation_id = ?", parsedAccommodationId)
		}
	}

	if err := tx.Limit(20).Find(&rates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi lấy danh sách đánh giá", "error": err.Error()})
		return
	}

	var rateResponses []RateResponse
	for _, rate := range rates {
		rateResponse := RateResponse{
			ID:              rate.ID,
			AccommodationID: rate.AccommodationID,
			Comment:         rate.Comment,
			Star:            rate.Star,
			CreateAt:        rate.CreateAt,
			UpdateAt:        rate.UpdateAt,
			User: UserInfo{
				ID:     rate.User.ID,
				Name:   rate.User.Name,
				Avatar: rate.User.Avatar,
			},
		}
		rateResponses = append(rateResponses, rateResponse)
	}

	rateResponsesJSON, err := json.Marshal(rateResponses)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi serialize dữ liệu", "error": err.Error()})
		return
	}

	if err := services.SetToRedis(config.Ctx, rdb, cacheKey, rateResponsesJSON, time.Hour); err != nil {
		log.Printf("Lỗi khi lưu danh sách đánh giá vào Redis: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách đánh giá thành công", "data": rateResponses})
}

func CreateRate(c *gin.Context) {
	var rate models.Rate
	if err := c.ShouldBindJSON(&rate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var existingRate models.Rate
	if err := config.DB.Where("user_id = ? AND accommodation_id = ?", rate.UserID, rate.AccommodationID).First(&existingRate).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "mess": "Bạn đã đánh giá lưu trú này trước đó"})
		return
	}

	if err := config.DB.Create(&rate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi tạo đánh giá", "error": err.Error()})
		return
	}

	if err := services.UpdateAccommodationRating(rate.AccommodationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update accommodation rating"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "rates:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
		cacheKey2 := "accommodations:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey2)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Tạo đánh giá thành công", "data": rate})
}

func GetRateDetail(c *gin.Context) {
	id := c.Param("id")
	var rate models.Rate
	if err := config.DB.Preload("User").First(&rate, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Đánh giá không tồn tại", "error": err.Error()})
		return
	}

	rateResponse := RateResponse{
		ID:              rate.ID,
		AccommodationID: rate.AccommodationID,
		Comment:         rate.Comment,
		Star:            rate.Star,
		CreateAt:        rate.CreateAt,
		UpdateAt:        rate.UpdateAt,
		User: UserInfo{
			ID:     rate.User.ID,
			Name:   rate.User.Name,
			Avatar: rate.User.Avatar,
		},
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy thông tin đánh giá thành công", "data": rateResponse})
}

func UpdateRate(c *gin.Context) {
	var rateInput struct {
		ID      uint   `json:"id"`
		Comment string `json:"comment"`
		Star    int    `json:"star"`
	}

	if err := c.ShouldBindJSON(&rateInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var rate models.Rate
	if err := config.DB.First(&rate, rateInput.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Đánh giá không tồn tại", "error": err.Error()})
		return
	}

	rate.Comment = rateInput.Comment
	rate.Star = rateInput.Star

	if err := config.DB.Save(&rate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi cập nhật đánh giá", "error": err.Error()})
		return
	}

	if err := services.UpdateAccommodationRating(rate.AccommodationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update accommodation rating"})
		return
	}

	rateResponse := RateUpdateResponse{
		ID:              rate.ID,
		AccommodationID: rate.AccommodationID,
		Comment:         rate.Comment,
		Star:            rate.Star,
		CreateAt:        rate.CreateAt,
		UpdateAt:        rate.UpdateAt,
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "rates:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
		cacheKey2 := "accommodations:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey2)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật đánh giá thành công", "data": rateResponse})
}
