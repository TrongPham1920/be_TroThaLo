package controllers

import (
	"log"
	"net/http"
	"net/url"
	"new/config"
	"new/models"
	"new/services"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HolidayResponse định nghĩa cấu trúc phản hồi cho kỳ nghỉ
type HolidayResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	FromDate  string    `json:"fromDate"`
	ToDate    string    `json:"toDate"`
	Price     int       `json:"price"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateHolidayRequest định nghĩa yêu cầu tạo kỳ nghỉ
type CreateHolidayRequest struct {
	ID       uint   `json:"id"`
	Name     string `json:"name" binding:"required"`
	FromDate string `json:"fromDate" binding:"required"`
	ToDate   string `json:"toDate" binding:"required"`
	Price    int    `json:"price" binding:"required"`
}

// GetHolidays lấy tất cả kỳ nghỉ
func GetHolidays(c *gin.Context) {
	cacheKey := "holidays:all"

	// Kết nối Redis
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis", "error": err.Error()})
		return
	}

	var holidays []models.Holiday
	err = services.GetFromRedis(config.Ctx, rdb, cacheKey, &holidays)
	if err == nil && len(holidays) > 0 {

		c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách kỳ nghỉ thành công từ cache", "data": holidays})
		return
	}
	if err := config.DB.Find(&holidays).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi lấy danh sách kỳ nghỉ"})
		return
	}
	if err := services.SetToRedis(config.Ctx, rdb, cacheKey, holidays, time.Hour); err != nil {
		log.Printf("Lỗi khi lưu danh sách kỳ nghỉ vào Redis: %v", err)
	}
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	nameFilter := c.Query("name")
	priceStr := c.Query("price")
	fromDateStr := c.Query("fromDate")
	toDateStr := c.Query("toDate")
	page := 0
	limit := 10

	if pageStr != "" {
		if parsedPage, err := strconv.Atoi(pageStr); err == nil && parsedPage >= 0 {
			page = parsedPage
		}
	}

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}
	var holidayResponses []HolidayResponse
	tx := config.DB.Model(&models.Holiday{})
	if nameFilter != "" {
		decodedNameFilter, err := url.QueryUnescape(nameFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể giải mã tham số name"})
			return
		}
		tx = tx.Where("name ILIKE ?", "%"+decodedNameFilter+"%")
	}
	if priceStr != "" {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err == nil {
			tx = tx.Where("price = ?", price)
		}
	}
	if fromDateStr != "" {
		fromDateComparable, err := ConvertDateToComparableFormat(fromDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Sai định dạng fromDate"})
			return
		}

		if toDateStr != "" {
			toDateComparable, err := ConvertDateToComparableFormat(toDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Sai định dạng toDate"})
				return
			}
			tx = tx.Where("SUBSTRING(from_date, 7, 4) || SUBSTRING(from_date, 4, 2) || SUBSTRING(from_date, 1, 2) >= ? AND SUBSTRING(to_date, 7, 4) || SUBSTRING(to_date, 4, 2) || SUBSTRING(to_date, 1, 2) <= ?", fromDateComparable, toDateComparable)
		} else {
			tx = tx.Where("SUBSTRING(from_date, 7, 4) || SUBSTRING(from_date, 4, 2) || SUBSTRING(from_date, 1, 2) >= ?", fromDateComparable)
		}
	}
	var totalHolidays int64
	if err := tx.Count(&totalHolidays).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể đếm số lượng ngày lễ"})
		return
	}
	tx = tx.Order("updated_at desc")

	if err := tx.Offset(page * limit).Limit(limit).Find(&holidays).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách ngày lễ"})
		return
	}

	for _, holiday := range holidays {
		holidayResponses = append(holidayResponses, HolidayResponse{
			ID:        holiday.ID,
			Name:      holiday.Name,
			FromDate:  holiday.FromDate,
			ToDate:    holiday.ToDate,
			Price:     holiday.Price,
			CreatedAt: holiday.CreatedAt,
			UpdatedAt: holiday.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách kỳ nghỉ thành công", "data": holidayResponses, "pagination": gin.H{
		"page":  page,
		"limit": limit,
		"total": totalHolidays,
	}})
}

// CreateHoliday tạo một kỳ nghỉ mới
func CreateHoliday(c *gin.Context) {
	var request CreateHolidayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}
	fromDate, err := time.Parse(layout, request.FromDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Định dạng ngày bắt đầu không hợp lệ"})
		return
	}
	toDate, err := time.Parse(layout, request.ToDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Định dạng ngày kết thúc không hợp lệ"})
		return
	}

	if toDate.Before(fromDate) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày kết thúc phải sau ngày bắt đầu"})
		return
	}
	holiday := models.Holiday{
		Name:      request.Name,
		FromDate:  request.FromDate,
		ToDate:    request.ToDate,
		Price:     request.Price,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := config.DB.Create(&holiday).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo kỳ nghỉ", "detail": err})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "holidays:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusCreated, gin.H{"code": 1, "mess": "Tạo kỳ nghỉ thành công", "data": holiday})
}
func GetDetailHoliday(c *gin.Context) {
	var holiday models.Holiday
	if err := config.DB.Where("id = ?", c.Param("id")).First(&holiday).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "message": "Không tìm thấy ngày lễ!"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "message": "Lấy thành công chi tiết ngày lễ!", "data": holiday})
}

// UpdateHoliday cập nhật một kỳ nghỉ
func UpdateHoliday(c *gin.Context) {
	var holiday models.Holiday
	var request CreateHolidayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}
	if err := config.DB.First(&holiday, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy ngày lễ"})
		return
	}

	holiday.Name = request.Name
	holiday.FromDate = request.FromDate
	holiday.ToDate = request.ToDate
	holiday.Price = request.Price
	holiday.UpdatedAt = time.Now()

	if err := config.DB.Save(&holiday).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật kỳ nghỉ"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "holidays:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật kỳ nghỉ thành công", "data": holiday})
}

func DeleteHoliday(c *gin.Context) {
	var request struct {
		IDs []uint `json:"ids"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}
	if len(request.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Không có ID nào được cung cấp"})
		return
	}

	if err := config.DB.Delete(&models.Holiday{}, request.IDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể xóa các kỳ nghỉ"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "holidays:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Xóa kỳ nghỉ thành công"})
}
