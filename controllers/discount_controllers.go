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

type DiscountResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	FromDate  string    `json:"fromDate"`
	ToDate    string    `json:"toDate"`
	Discount  int       `json:"discount"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateDiscountRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description" binding:"required"`
	Quantity    int    `json:"quantity" binding:"required"`
	FromDate    string `json:"fromDate" binding:"required"`
	ToDate      string `json:"toDate" binding:"required"`
	Discount    int    `json:"discount" binding:"required"`
}

type UpdateDiscountRequest struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	FromDate    string `json:"fromDate"`
	ToDate      string `json:"toDate"`
	Discount    int    `json:"discount"`
	Status      int    `json:"status"`
}

type ChangeDiscountStatusRequest struct {
	ID     uint `json:"id"`
	Status int  `json:"status"`
}

var layout = "02/01/2006"

func ConvertDateToComparableFormat(dateStr string) (string, error) {
	parsedDate, err := time.Parse(layout, dateStr)
	if err != nil {
		return "", err
	}
	return parsedDate.Format("20060102"), nil
}
func GetDiscounts(c *gin.Context) {
	cacheKey := "discounts:all"
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis", "error": err.Error()})
		return
	}
	var discounts []models.Discount
	err = services.GetFromRedis(config.Ctx, rdb, cacheKey, &discounts)
	if err == nil && len(discounts) > 0 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách giảm giá thành công từ cache", "data": discounts})
		return
	}

	currentDate := time.Now()

	if err := config.DB.Find(&discounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi lấy danh sách chương trình giảm giá"})
		return
	}

	for i, discount := range discounts {

		toDate, err := time.Parse("02/01/2006", discount.ToDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Định dạng ngày không hợp lệ trong mã giảm giá"})
			return
		}

		if discount.ID != 1 && toDate.Before(currentDate) && discount.Status == 1 {
			discounts[i].Status = 0
			if err := config.DB.Save(&discounts[i]).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật trạng thái mã giảm giá đã hết hạn"})
				return
			}
		}
	}
	// Lưu vào Redis
	if err := services.SetToRedis(config.Ctx, rdb, cacheKey, discounts, time.Hour); err != nil {
		log.Printf("Lỗi khi lưu danh sách giảm giá vào Redis: %v", err)
	}
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	statusFilter := c.Query("status")
	nameFilter := c.Query("name")
	discountStr := c.Query("discount")
	quantityStr := c.Query("quantity")
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

	var discountResponses []DiscountResponse

	tx := config.DB.Model(&models.Discount{})
	if nameFilter != "" {
		decodedNameFilter, err := url.QueryUnescape(nameFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể giải mã tham số name"})
			return
		}
		tx = tx.Where("name ILIKE ?", "%"+decodedNameFilter+"%")
	}
	if statusFilter != "" {
		tx = tx.Where("status = ?", statusFilter)
	}
	if discountStr != "" {
		discount, err := strconv.ParseFloat(discountStr, 64)
		if err == nil {
			tx = tx.Where("discount = ?", discount)
		}
	}
	if quantityStr != "" {
		quantity, err := strconv.ParseFloat(quantityStr, 64)
		if err == nil {
			tx = tx.Where("quantity = ?", quantity)
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

	var totalDiscounts int64
	if err := tx.Count(&totalDiscounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể đếm số lượng mã giảm giá"})
		return
	}
	tx = tx.Order("updated_at desc")

	if err := tx.Offset(page * limit).Limit(limit).Find(&discounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách mã giảm giá"})
		return
	}
	for _, discount := range discounts {
		discountResponses = append(discountResponses, DiscountResponse{
			ID:        discount.ID,
			Name:      discount.Name,
			Quantity:  discount.Quantity,
			FromDate:  discount.FromDate,
			ToDate:    discount.ToDate,
			Discount:  discount.Discount,
			Status:    discount.Status,
			CreatedAt: discount.CreatedAt,
			UpdatedAt: discount.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy danh sách chương trình giảm giá thành công", "data": discountResponses,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": totalDiscounts,
		}})
}
func GetDiscountDetail(c *gin.Context) {
	var discount models.Discount
	discountId := c.Param("id")
	if err := config.DB.Where("id = ?", discountId).First(&discount).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy mã giảm giá!"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy thông tin chi tiết của mã giảm giá thành công", "data": discount})
}
func CreateDiscount(c *gin.Context) {
	var request CreateDiscountRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}

	if request.Discount < 0 || request.Discount > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Mức giảm giá phải nằm trong khoảng từ 0 đến 100"})
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

	if !toDate.After(fromDate) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày kết thúc phải sau ngày bắt đầu"})
		return
	}
	discount := models.Discount{
		Name:        request.Name,
		Description: request.Description,
		Quantity:    request.Quantity,
		FromDate:    request.FromDate,
		ToDate:      request.ToDate,
		Discount:    request.Discount,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := config.DB.Create(&discount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo chương trình giảm giá", "detail": err})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}
	c.JSON(http.StatusCreated, gin.H{"code": 1, "mess": "Tạo chương trình giảm giá thành công", "data": discount})
}

func UpdateDiscount(c *gin.Context) {
	var request UpdateDiscountRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}

	var discount models.Discount
	if err := config.DB.First(&discount, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Chương trình giảm giá không tồn tại"})
		return
	}

	if request.Name != "" {
		discount.Name = request.Name
	}
	if request.Description != "" {
		discount.Description = request.Description
	}
	if request.Quantity > 0 {
		discount.Quantity = request.Quantity
	}
	if request.FromDate != "" {
		discount.FromDate = request.FromDate
	}
	if request.ToDate != "" {
		discount.ToDate = request.ToDate
	}
	if request.Discount > 0 {
		discount.Discount = request.Discount
	}
	discount.UpdatedAt = time.Now()
	discount.Status = request.Status

	if err := config.DB.Save(&discount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật chương trình giảm giá"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật chương trình giảm giá thành công", "data": discount})
}

func DeleteDiscount(c *gin.Context) {
	id := c.Param("id")
	if err := config.DB.Delete(&models.Discount{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể xóa chương trình giảm giá"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Xóa chương trình giảm giá thành công"})
}

func ChangeDiscountStatus(c *gin.Context) {
	var request ChangeDiscountStatusRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var discount models.Discount
	if err := config.DB.First(&discount, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy mã giảm giá", "error": err.Error()})
		return
	}

	discount.Status = request.Status

	if err := discount.ValidateStatusDiscount(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	if err := config.DB.Model(&discount).Update("status", request.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể thay đổi trạng thái mã giảm giá", "error": err.Error()})
		return
	}

	discount.Status = request.Status

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Thay đổi trạng thái mã giảm giá thành công", "data": discount})
}
