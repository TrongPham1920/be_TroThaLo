package controllers

import (
	"log"
	"net/http"
	"net/url"
	"new/config"
	"new/models"
	"new/services"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type UpdateBenefitRequest struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type CreateBenefitRequest struct {
	Name string `json:"name" binding:"required"`
}

type ChangeBenefitStatusRequest struct {
	ID     uint `json:"id"`
	Status int  `json:"status"`
}

type BenefitResponse struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// Lọc benefit theo status
func filterBenefitsByStatus(benefits []models.Benefit, status int) []BenefitResponse {
	var filtered []BenefitResponse
	for _, b := range benefits {
		if b.Status == status {
			filtered = append(filtered, BenefitResponse{
				Id:   b.Id,
				Name: b.Name,
			})
		}
	}
	return filtered
}

// Lọc Benefit cho cms
func filterBenefits(benefits []models.Benefit, statusFilter, nameFilter string) []BenefitResponse {
	var filtered []BenefitResponse
	for _, b := range benefits {
		// Filter theo status
		if statusFilter != "" {
			parsedStatus, err := strconv.Atoi(statusFilter)
			if err == nil && b.Status != parsedStatus {
				continue
			}
		}

		// Filter theo name
		if nameFilter != "" {
			decodedNameFilter, _ := url.QueryUnescape(nameFilter)
			if !strings.Contains(strings.ToLower(b.Name), strings.ToLower(decodedNameFilter)) {
				continue
			}
		}

		filtered = append(filtered, BenefitResponse{
			Id:   b.Id,
			Name: b.Name,
		})
	}
	return filtered
}

func GetAllBenefit(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	currentUserRole := 0
	if authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		_, role, err := GetUserIDFromToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
			return
		}
		currentUserRole = role
	}

	statusFilter := c.Query("status")
	nameFilter := c.Query("name")
	pageStr := c.Query("page")
	limitStr := c.Query("limit")

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

	// Redis cache key
	cacheKey := "benefits:all"
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis", "error": err.Error()})
		return
	}

	var allBenefits []models.Benefit

	err = services.GetFromRedis(config.Ctx, rdb, cacheKey, &allBenefits)
	if err != nil || len(allBenefits) == 0 {

		if err := config.DB.Find(&allBenefits).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách lợi ích", "error": err.Error()})
			return
		}

		// Lưu vào Redis
		if err := services.SetToRedis(config.Ctx, rdb, cacheKey, allBenefits, time.Hour); err != nil {
			log.Printf("Lỗi khi lưu danh sách lợi ích vào Redis: %v", err)
		}
	}

	var filteredBenefits []BenefitResponse

	//filter role = 1,2,3 cho sidebar cms, còn lại filter cho web user
	if currentUserRole != 0 {
		filteredBenefits = filterBenefits(allBenefits, statusFilter, nameFilter)
	} else {
		filteredBenefits = filterBenefitsByStatus(allBenefits, 0)
	}

	// Pagination
	total := len(filteredBenefits)
	if currentUserRole == 0 {
		// Nếu userRole là 0, không áp dụng phân trang, trả về tất cả dữ liệu
		c.JSON(http.StatusOK, gin.H{
			"code":  1,
			"mess":  "Lấy danh sách tiện ích thành công",
			"data":  filteredBenefits,
			"total": total,
		})
		return
	}

	//Các user khác phân trang
	start := page * limit
	end := start + limit

	if start >= total {
		filteredBenefits = []BenefitResponse{}
	} else if end > total {
		filteredBenefits = filteredBenefits[start:]
	} else {
		filteredBenefits = filteredBenefits[start:end]
	}

	// Trả về kết quả
	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy danh sách tiện ích thành công",
		"data": filteredBenefits,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": total,
		},
	})
}

func CreateBenefit(c *gin.Context) {
	var benefitRequests []CreateBenefitRequest

	if err := c.ShouldBindJSON(&benefitRequests); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var benefit []models.Benefit
	for _, benefitRequest := range benefitRequests {
		benefit = append(benefit, models.Benefit{Name: benefitRequest.Name})
	}
	if err := config.DB.Create(&benefit).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo lợi ích", "error": err.Error()})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Tạo lợi ích thành công", "data": benefit})
}

func GetBenefitDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "ID không hợp lệ", "error": err.Error()})
		return
	}

	var benefit models.Benefit
	if err := config.DB.First(&benefit, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy lợi ích", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy thông tin lợi ích thành công",
		"data": benefit,
	})
}

func UpdateBenefit(c *gin.Context) {
	var request UpdateBenefitRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var benefit models.Benefit
	if err := config.DB.First(&benefit, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy lợi ích", "error": err.Error()})
		return
	}

	benefit.Name = request.Name

	if err := config.DB.Save(&benefit).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật lợi ích", "error": err.Error()})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật lợi ích thành công", "data": benefit})
}

func ChangeBenefitStatus(c *gin.Context) {
	var request ChangeBenefitStatusRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var benefit models.Benefit
	if err := config.DB.First(&benefit, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Không tìm thấy lợi ích", "error": err.Error()})
		return
	}

	benefit.Status = request.Status

	if err := benefit.ValidateStatus(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	if err := config.DB.Model(&benefit).Update("status", request.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể thay đổi trạng thái lợi ích", "error": err.Error()})
		return
	}

	benefit.Status = request.Status

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "benefits:all"
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Thay đổi trạng thái lợi ích thành công", "data": benefit})
}
