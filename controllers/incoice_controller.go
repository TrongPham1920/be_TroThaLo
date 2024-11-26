package controllers

import (
	"database/sql"
	"fmt"
	"net/http"
	"new/config"
	"new/models"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"

	"github.com/gin-gonic/gin"
)

type InvoiceResponse struct {
	ID              uint         `json:"id"`
	InvoiceCode     string       `json:"invoiceCode"`
	OrderID         uint         `json:"orderId"`
	TotalAmount     float64      `json:"totalAmount"`
	PaidAmount      float64      `json:"paidAmount"`
	RemainingAmount float64      `json:"remainingAmount"`
	Status          int          `json:"status"`
	PaymentDate     *string      `json:"paymentDate,omitempty"`
	CreatedAt       string       `json:"createdAt"`
	UpdatedAt       string       `json:"updatedAt"`
	User            UserResponse `json:"user"`
}

func GetInvoices(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	currentUserID, currentUserRole, err := GetUserIDFromToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
		return
	}

	var invoices []models.Invoice
	var invoiceResponses []InvoiceResponse
	var totalInvoices int64

	pageStr := c.DefaultQuery("page", "0")
	limitStr := c.DefaultQuery("limit", "10")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 0 {
		page = 0
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}

	redisClient, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to connect to Redis"})
		return
	}

	cacheKey := fmt.Sprintf("invoices:all:page=%d:limit=%d", page, limit)

	cachedData, err := redisClient.Get(config.Ctx, cacheKey).Result()
	if err == nil && cachedData != "" {
		var cachedResponse gin.H
		if json.Unmarshal([]byte(cachedData), &cachedResponse) == nil {
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}

	tx := config.DB.Model(&models.Invoice{})
	if currentUserRole == 2 {
		tx = tx.Where("order_id IN (?)", config.DB.Table("orders").
			Select("orders.id").
			Joins("JOIN accommodations ON accommodations.id = orders.accommodation_id").
			Where("accommodations.user_id = ?", currentUserID))
	} else if currentUserRole == 3 {
		var user models.User
		if err := config.DB.Where("id = ?", currentUserID).First(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tìm thấy người dùng"})
			return
		}
		tx = tx.Where("order_id IN (?)", config.DB.Table("orders").
			Select("orders.id").
			Joins("JOIN accommodations ON accommodations.id = orders.accommodation_id").
			Where("accommodations.user_id = ?", user.AdminId))
	}

	if err := tx.Count(&totalInvoices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to count invoices"})
		return
	}

	if err := tx.Order("updated_at DESC").
		Offset(page * limit).
		Limit(limit).
		Find(&invoices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to fetch invoices"})
		return
	}

	for _, invoice := range invoices {
		invoiceResponses = append(invoiceResponses, InvoiceResponse{
			ID:              invoice.ID,
			InvoiceCode:     invoice.InvoiceCode,
			OrderID:         invoice.OrderID,
			TotalAmount:     invoice.TotalAmount,
			PaidAmount:      invoice.PaidAmount,
			RemainingAmount: invoice.RemainingAmount,
			Status:          invoice.Status,
			PaymentDate:     nil,
			CreatedAt:       invoice.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:       invoice.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	responseData := gin.H{
		"code": 1,
		"mess": "Invoices fetched successfully",
		"data": invoiceResponses,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": totalInvoices,
		},
	}

	jsonData, err := json.Marshal(responseData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Error processing data"})
		return
	}

	ttl := 15 * time.Minute

	err = redisClient.Set(config.Ctx, cacheKey, jsonData, ttl).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to save cache"})
		return
	}

	c.JSON(http.StatusOK, responseData)
}

func GetDetailInvoice(c *gin.Context) {
	var invoice models.Invoice
	if err := config.DB.Where("id = ?", c.Param("id")).First(&invoice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "message": "Không tìm thấy hóa đơn!"})
		return
	}
	var order models.Order
	if err := config.DB.Where("id = ?", invoice.OrderID).First(&order).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "message": "Không tìm thấy đơn hàng liên quan!"})
		return
	}
	var user models.User
	if err := config.DB.Where("id = ?", order.UserID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "message": "Không tìm thấy người dùng liên quan!"})
		return
	}
	invoiceResponse := InvoiceResponse{
		ID:              invoice.ID,
		InvoiceCode:     invoice.InvoiceCode,
		OrderID:         invoice.OrderID,
		TotalAmount:     invoice.TotalAmount,
		PaidAmount:      invoice.PaidAmount,
		RemainingAmount: invoice.RemainingAmount,
		Status:          invoice.Status,
		PaymentDate:     nil,
		CreatedAt:       invoice.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:       invoice.UpdatedAt.Format("2006-01-02 15:04:05"),
		User: UserResponse{
			UserID:    user.ID,
			UserEmail: user.Email,
			UserPhone: user.PhoneNumber,
		},
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "message": "Lấy chi tiết hóa đơn thành công", "data": invoiceResponse})
}

type MonthRevenue struct {
	Month      string  `json:"month"`
	Revenue    float64 `json:"revenue"`
	OrderCount int     `json:"orderCount"`
}

type RevenueResponse struct {
	TotalRevenue        float64        `json:"totalRevenue"`
	CurrentMonthRevenue float64        `json:"currentMonthRevenue"`
	LastMonthRevenue    float64        `json:"lastMonthRevenue"`
	CurrentWeekRevenue  float64        `json:"currentWeekRevenue"`
	MonthlyRevenue      []MonthRevenue `json:"monthlyRevenue"`
}

func GetTotalRevenue(c *gin.Context) {
	var totalRevenue, currentMonthRevenue, currentWeekRevenue float64
	var lastMonthRevenue sql.NullFloat64
	var monthlyRevenue []MonthRevenue

	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	currentUserID, currentUserRole, err := GetUserIDFromToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
		return
	}

	redisClient, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to connect to Redis"})
		return
	}

	cacheKey := "total_revenue"
	cachedData, err := redisClient.Get(config.Ctx, cacheKey).Result()
	if err == nil && cachedData != "" {
		var cachedResponse RevenueResponse
		if json.Unmarshal([]byte(cachedData), &cachedResponse) == nil {
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}

	tx := config.DB.Model(&models.Invoice{})
	if currentUserRole == 2 {
		tx = tx.Where("order_id IN (?)", config.DB.Table("orders").
			Select("orders.id").
			Joins("JOIN accommodations ON accommodations.id = orders.accommodation_id").
			Where("accommodations.user_id = ?", currentUserID))
	} else if currentUserRole == 3 {
		var user models.User
		if err := config.DB.Where("id = ?", currentUserID).First(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tìm thấy người dùng"})
			return
		}
		tx = tx.Where("order_id IN (?)", config.DB.Table("orders").
			Select("orders.id").
			Joins("JOIN accommodations ON accommodations.id = orders.accommodation_id").
			Where("accommodations.user_id = ?", user.AdminId))
	}

	var invoices []models.Invoice
	if err := tx.Find(&invoices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Không thể lấy danh sách hóa đơn"})
		return
	}

	for _, invoice := range invoices {
		totalRevenue += invoice.TotalAmount

		currentMonth := time.Now().Format("2006-01")
		if invoice.CreatedAt.Format("2006-01") == currentMonth {
			currentMonthRevenue += invoice.TotalAmount
		}

		lastMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
		if invoice.CreatedAt.Format("2006-01") == lastMonth {
			lastMonthRevenue.Float64 += invoice.TotalAmount
		}

		currentWeekStart := time.Now().AddDate(0, 0, -int(time.Now().Weekday()))
		currentWeekEnd := currentWeekStart.AddDate(0, 0, 6)
		if invoice.CreatedAt.After(currentWeekStart) && invoice.CreatedAt.Before(currentWeekEnd) {
			currentWeekRevenue += invoice.TotalAmount
		}
	}

	currentYear := time.Now().Year()
	for i := 1; i <= 12; i++ {
		month := fmt.Sprintf("%d-%02d", currentYear, i)
		var revenue, orderCount float64

		for _, invoice := range invoices {
			if invoice.CreatedAt.Format("2006-01") == month {
				revenue += invoice.TotalAmount
				orderCount++
			}
		}

		monthlyRevenue = append(monthlyRevenue, MonthRevenue{
			Month:      fmt.Sprintf("Tháng %d", i),
			Revenue:    revenue,
			OrderCount: int(orderCount),
		})
	}

	if currentUserRole == 1 {
		totalRevenue *= 0.30
		currentMonthRevenue *= 0.30
		lastMonthRevenue.Float64 *= 0.30
		currentWeekRevenue *= 0.30
		for i := range monthlyRevenue {
			monthlyRevenue[i].Revenue *= 0.30
		}
	}

	response := RevenueResponse{
		TotalRevenue:        totalRevenue,
		CurrentMonthRevenue: currentMonthRevenue,
		LastMonthRevenue:    lastMonthRevenue.Float64,
		CurrentWeekRevenue:  currentWeekRevenue,
		MonthlyRevenue:      monthlyRevenue,
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Error processing data"})
		return
	}

	ttl := 15 * time.Minute
	err = redisClient.Set(config.Ctx, cacheKey, jsonData, ttl).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Unable to save cache"})
		return
	}

	c.JSON(http.StatusOK, response)
}

func UpdatePaymentStatus(c *gin.Context) {
	var request struct {
		ID          uint `json:"id"`
		PaymentType int  `json:"paymentType"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu yêu cầu không hợp lệ"})
		return
	}

	var invoice models.Invoice
	if err := config.DB.Where("id = ?", request.ID).First(&invoice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Hóa đơn không tìm thấy"})
		return
	}

	invoice.PaymentType = &request.PaymentType
	currentTime := time.Now()
	invoice.PaymentDate = &currentTime
	invoice.Status = 1

	if err := config.DB.Save(&invoice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật trạng thái thanh toán"})
		return
	}

	redisClient, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	cacheKeyPattern := "invoices:*"
	keys, err := redisClient.Keys(config.Ctx, cacheKeyPattern).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tìm kiếm cache Redis"})
		return
	}

	if len(keys) > 0 {
		err := redisClient.Del(config.Ctx, keys...).Err()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể xóa cache Redis"})
			return
		}
	}

	page, limit := 0, 10
	cacheKey := fmt.Sprintf("invoices:all:page=%d:limit=%d", page, limit)

	var invoices []models.Invoice
	var invoiceResponses []InvoiceResponse
	var totalInvoices int64

	tx := config.DB.Model(&models.Invoice{})
	if err := tx.Count(&totalInvoices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể đếm số lượng hóa đơn"})
		return
	}

	if err := tx.Offset(page * limit).Limit(limit).Find(&invoices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách hóa đơn"})
		return
	}

	for _, invoice := range invoices {
		invoiceResponses = append(invoiceResponses, InvoiceResponse{
			ID:              invoice.ID,
			InvoiceCode:     invoice.InvoiceCode,
			OrderID:         invoice.OrderID,
			TotalAmount:     invoice.TotalAmount,
			PaidAmount:      invoice.PaidAmount,
			RemainingAmount: invoice.RemainingAmount,
			Status:          invoice.Status,
			PaymentDate:     nil,
			CreatedAt:       invoice.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:       invoice.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	responseData := gin.H{
		"code": 1,
		"mess": "Cập nhật trạng thái thanh toán thành công",
		"data": invoiceResponses,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": totalInvoices,
		},
	}

	jsonData, err := json.Marshal(responseData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi xử lý dữ liệu"})
		return
	}

	ttl := 15 * time.Minute
	err = redisClient.Set(config.Ctx, cacheKey, jsonData, ttl).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lưu cache Redis"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Cập nhật trạng thái thanh toán thành công",
	})
}
