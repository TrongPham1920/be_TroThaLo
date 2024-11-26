package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"new/config"
	"new/models"
	"new/services"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"
)

type AccommodationRequest struct {
	ID               uint             `json:"id"`
	Type             int              `json:"type"`
	Name             string           `json:"name"`
	Address          string           `json:"address"`
	Avatar           string           `json:"avatar"`
	Img              json.RawMessage  `json:"img" gorm:"type:json"`
	ShortDescription string           `json:"shortDescription"`
	Description      string           `json:"description"`
	Status           int              `json:"status"`
	Num              int              `json:"num"`
	Furniture        json.RawMessage  `json:"furniture" gorm:"type:json"`
	Benefits         []models.Benefit `json:"benefits" gorm:"many2many:accommodation_benefits;"`
	People           int              `json:"people"`
	Price            int              `json:"price"`
	TimeCheckOut     string           `json:"timeCheckOut"`
	TimeCheckIn      string           `json:"timeCheckIn"`
	Province         string           `json:"province"`
	District         string           `json:"district"`
	Ward             string           `json:"ward"`
	Longitude        float64          `json:"longitude"`
	Latitude         float64          `json:"latitude"`
}

type Actor struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	PhoneNumber   string `json:"phoneNumber"`
	BankName      string `json:"bankName"`
	AccountNumber string `json:"accountNumber"`
	BankShortName string `json:"bankShortName"`
}

type AccommodationResponse struct {
	ID               uint             `json:"id"`
	Type             int              `json:"type"`
	Province         string           `json:"province"`
	Name             string           `json:"name"`
	Address          string           `json:"address"`
	CreateAt         time.Time        `json:"createAt"`
	UpdateAt         time.Time        `json:"updateAt"`
	Avatar           string           `json:"avatar"`
	ShortDescription string           `json:"shortDescription"`
	Status           int              `json:"status"`
	Num              int              `json:"num"`
	People           int              `json:"people"`
	Price            int              `json:"price"`
	NumBed           int              `json:"numBed"`
	NumTolet         int              `json:"numTolet"`
	TimeCheckOut     string           `json:"timeCheckOut"`
	TimeCheckIn      string           `json:"timeCheckIn"`
	District         string           `json:"district"`
	Ward             string           `json:"ward"`
	Longitude        float64          `json:"longitude"`
	Latitude         float64          `json:"latitude"`
	Benefits         []models.Benefit `json:"benefits"`
}

type AccommodationDetailResponse struct {
	ID               uint             `json:"id"`
	Type             int              `json:"type"`
	Province         string           `json:"province"`
	District         string           `json:"district"`
	Ward             string           `json:"ward"`
	Name             string           `json:"name"`
	Address          string           `json:"address"`
	CreateAt         time.Time        `json:"createAt"`
	UpdateAt         time.Time        `json:"updateAt"`
	Avatar           string           `json:"avatar"`
	ShortDescription string           `json:"shortDescription"`
	Description      string           `json:"description"`
	Status           int              `json:"status"`
	User             Actor            `json:"user"`
	Num              int              `json:"num"`
	People           int              `json:"people"`
	Price            int              `json:"price"`
	NumBed           int              `json:"numBed"`
	NumTolet         int              `json:"numTolet"`
	Furniture        json.RawMessage  `json:"furniture" gorm:"type:json"`
	Img              json.RawMessage  `json:"img"`
	Benefits         []models.Benefit `json:"benefits"`
	Rates            []RateResponse   `json:"rates"`
	TimeCheckOut     string           `json:"timeCheckOut"`
	TimeCheckIn      string           `json:"timeCheckIn"`
	Longitude        float64          `json:"longitude"`
	Latitude         float64          `json:"latitude"`
}

func GetAllAccommodations(c *gin.Context) {
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

	// Tạo cache key dựa trên vai trò và user_id
	var cacheKey string
	if currentUserRole == 2 {
		cacheKey = fmt.Sprintf("accommodations:admin:%d", currentUserID)
	} else if currentUserRole == 3 {
		cacheKey = fmt.Sprintf("accommodations:receptionist:%d", currentUserID)
	} else {
		cacheKey = "accommodations:all"
	}

	// Kết nối Redis
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	var allAccommodations []models.Accommodation

	// Lấy dữ liệu từ Redis
	if err := services.GetFromRedis(config.Ctx, rdb, cacheKey, &allAccommodations); err != nil || len(allAccommodations) == 0 {
		tx := config.DB.Model(&models.Accommodation{}).
			Preload("Rooms").
			Preload("Rates").
			Preload("Benefits").
			Preload("User").
			Preload("User.Banks")
		if currentUserRole == 2 {
			//Lấy data theo vai trò Admin (Role = 2)
			tx = tx.Where("user_id = ?", currentUserID)
		} else if currentUserRole == 3 {
			//Lấy data theo vai trò Receptionist (Role = 3)
			var adminID int
			if err := config.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err != nil {
				c.JSON(http.StatusForbidden, gin.H{"code": 0, "mess": "Không có quyền truy cập"})
				return
			}
			tx = tx.Where("user_id = ?", adminID)
		}

		// Lấy dữ liệu từ DB
		if err := tx.Find(&allAccommodations).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách chỗ ở"})
			return
		}

		// Lưu dữ liệu vào Redis
		if err := services.SetToRedis(config.Ctx, rdb, cacheKey, allAccommodations, time.Hour); err != nil {
			log.Printf("Lỗi khi lưu danh sách chỗ ở vào Redis: %v", err)
		}
	}

	// Áp dụng filter từ dữ liệu cache
	typeFilter := c.Query("type")
	statusFilter := c.Query("status")
	nameFilter := c.Query("name")
	numBedFilter := c.Query("numBed")
	numToletFilter := c.Query("numTolet")
	peopleFilter := c.Query("people")
	provinceFilter := c.Query("province")

	filteredAccommodations := make([]models.Accommodation, 0)
	for _, acc := range allAccommodations {
		if typeFilter != "" {
			parsedTypeFilter, err := strconv.Atoi(typeFilter)
			if err == nil && acc.Type != parsedTypeFilter {
				continue
			}
		}
		if statusFilter != "" {
			parsedStatusFilter, err := strconv.Atoi(statusFilter)
			if err == nil && acc.Status != parsedStatusFilter {
				continue
			}
		}
		if provinceFilter != "" {
			decodedProvinceFilter, _ := url.QueryUnescape(provinceFilter)
			if !strings.Contains(strings.ToLower(acc.Name), strings.ToLower(decodedProvinceFilter)) {
				continue
			}
		}
		if nameFilter != "" {
			decodedNameFilter, _ := url.QueryUnescape(nameFilter)
			if !strings.Contains(strings.ToLower(acc.Name), strings.ToLower(decodedNameFilter)) {
				continue
			}
		}
		if numBedFilter != "" {
			numBed, _ := strconv.Atoi(numBedFilter)
			if acc.NumBed != numBed {
				continue
			}
		}
		if numToletFilter != "" {
			numTolet, _ := strconv.Atoi(numToletFilter)
			if acc.NumTolet != numTolet {
				continue
			}
		}
		if peopleFilter != "" {
			people, _ := strconv.Atoi(peopleFilter)
			if acc.People != people {
				continue
			}
		}
		filteredAccommodations = append(filteredAccommodations, acc)
	}
	total := len(filteredAccommodations)

	//Xếp theo update mới nhất
	sort.Slice(filteredAccommodations, func(i, j int) bool {
		return filteredAccommodations[i].UpdateAt.After(filteredAccommodations[j].UpdateAt)
	})
	// Pagination
	page := 0
	limit := 10
	if pageStr := c.Query("page"); pageStr != "" {
		if parsedPage, err := strconv.Atoi(pageStr); err == nil && parsedPage >= 0 {
			page = parsedPage
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	start := page * limit
	end := start + limit
	if start >= len(filteredAccommodations) {
		filteredAccommodations = []models.Accommodation{}
	} else if end > len(filteredAccommodations) {
		filteredAccommodations = filteredAccommodations[start:]
	} else {
		filteredAccommodations = filteredAccommodations[start:end]
	}

	// Chuẩn bị response
	accommodationsResponse := make([]AccommodationResponse, 0)
	for _, acc := range filteredAccommodations {
		accommodationsResponse = append(accommodationsResponse, AccommodationResponse{
			ID:               acc.ID,
			Type:             acc.Type,
			Name:             acc.Name,
			Address:          acc.Address,
			CreateAt:         acc.CreateAt,
			UpdateAt:         acc.UpdateAt,
			Avatar:           acc.Avatar,
			ShortDescription: acc.ShortDescription,
			Status:           acc.Status,
			Num:              acc.Num,
			People:           acc.People,
			Price:            acc.Price,
			NumBed:           acc.NumBed,
			NumTolet:         acc.NumTolet,
			Province:         acc.Province,
			District:         acc.District,
			Ward:             acc.Ward,
			Longitude:        acc.Longitude,
			Latitude:         acc.Latitude,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy danh sách chỗ ở thành công",
		"data": accommodationsResponse,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": total,
		},
	})
}

func GetAllAccommodationsForUser(c *gin.Context) {
	// Các tham số filter
	typeFilter := c.Query("type")
	provinceFilter := c.Query("province")
	districtFilter := c.Query("district")
	benefitFilterRaw := c.Query("benefitId")
	numFilter := c.Query("num")
	statusFilter := c.Query("status")
	nameFilter := c.Query("name")
	numBedFilter := c.Query("numBed")
	numToletFilter := c.Query("numTolet")
	peopleFilter := c.Query("people")

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
	cacheKey := "accommodations:all"
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	var allAccommodations []models.Accommodation

	// Lấy dữ liệu từ Redis
	if err := services.GetFromRedis(config.Ctx, rdb, cacheKey, &allAccommodations); err != nil || len(allAccommodations) == 0 {
		// Nếu không có dữ liệu trong Redis, lấy từ Database
		if err := config.DB.Model(&models.Accommodation{}).
			Preload("Rooms").
			Preload("Rates").
			Preload("Benefits").
			Preload("User").
			Preload("User.Banks").
			Find(&allAccommodations).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách chỗ ở"})
			return
		}

		// Lưu dữ liệu vào Redis
		if err := services.SetToRedis(config.Ctx, rdb, cacheKey, allAccommodations, time.Hour); err != nil {
			log.Printf("Lỗi khi lưu danh sách chỗ ở vào Redis: %v", err)
		}
	}
	benefitIDs := make([]int, 0)
	//chuyển đổi thành slice int (query mặc đinh string)
	if benefitFilterRaw != "" {
		// Loại bỏ các ký tự "[" và "]"
		benefitFilterRaw = strings.Trim(benefitFilterRaw, "[]")

		// Tách các phần tử bằng dấu phẩy
		benefitStrs := strings.Split(benefitFilterRaw, ",")

		// Chuyển đổi từng phần tử thành int
		for _, benefitStr := range benefitStrs {
			if benefitID, err := strconv.Atoi(strings.TrimSpace(benefitStr)); err == nil {
				benefitIDs = append(benefitIDs, benefitID)
			}
		}
	}

	// Áp dụng filter trên dữ liệu từ Redis
	filteredAccommodations := make([]models.Accommodation, 0)
	for _, acc := range allAccommodations {
		if typeFilter != "" {
			parsedTypeFilter, err := strconv.Atoi(typeFilter)
			if err == nil && acc.Type != parsedTypeFilter {
				continue
			}
		}

		if statusFilter != "" {
			parsedStatusFilter, err := strconv.Atoi(statusFilter)
			if err == nil && acc.Status != parsedStatusFilter {
				continue
			}
		}

		if provinceFilter != "" {
			decodedProvinceFilter, _ := url.QueryUnescape(provinceFilter)
			if !strings.Contains(strings.ToLower(acc.Province), strings.ToLower(decodedProvinceFilter)) {
				continue
			}
		}

		if districtFilter != "" {
			decodedDistrictFilter, _ := url.QueryUnescape(districtFilter)
			if !strings.Contains(strings.ToLower(acc.District), strings.ToLower(decodedDistrictFilter)) {
				continue
			}
		}
		if nameFilter != "" {
			decodedNameFilter, _ := url.QueryUnescape(nameFilter)
			if !strings.Contains(strings.ToLower(acc.Name), strings.ToLower(decodedNameFilter)) {
				continue
			}
		}

		if numBedFilter != "" {
			numBed, _ := strconv.Atoi(numBedFilter)
			if acc.NumBed != numBed {
				continue
			}
		}
		if numToletFilter != "" {
			numTolet, _ := strconv.Atoi(numToletFilter)
			if acc.NumTolet != numTolet {
				continue
			}
		}
		if peopleFilter != "" {
			people, _ := strconv.Atoi(peopleFilter)
			if acc.People != people {
				continue
			}
		}

		if numFilter != "" {
			num, _ := strconv.Atoi(numFilter)
			if acc.Num != num {
				continue
			}
		}
		if len(benefitIDs) > 0 {
			match := false
			for _, benefit := range acc.Benefits {
				for _, id := range benefitIDs {
					if benefit.Id == id {
						match = true
						break
					}
				}
				if match {
					break
				}
			}
			if !match {
				continue
			}
		}
		filteredAccommodations = append(filteredAccommodations, acc)
	}

	//Xếp theo update mới nhất
	sort.Slice(filteredAccommodations, func(i, j int) bool {
		return filteredAccommodations[i].UpdateAt.After(filteredAccommodations[j].UpdateAt)
	})

	// Pagination
	// Lấy total sau khi lọc
	total := len(filteredAccommodations)

	// Áp dụng phân trang
	start := page * limit
	end := start + limit
	if start >= total {
		filteredAccommodations = []models.Accommodation{}
	} else if end > total {
		filteredAccommodations = filteredAccommodations[start:]
	} else {
		filteredAccommodations = filteredAccommodations[start:end]
	}

	// Chuẩn bị response
	accommodationsResponse := make([]AccommodationResponse, 0)
	for _, acc := range filteredAccommodations {
		accommodationsResponse = append(accommodationsResponse, AccommodationResponse{
			ID:               acc.ID,
			Type:             acc.Type,
			Name:             acc.Name,
			Address:          acc.Address,
			CreateAt:         acc.CreateAt,
			UpdateAt:         acc.UpdateAt,
			Avatar:           acc.Avatar,
			ShortDescription: acc.ShortDescription,
			Status:           acc.Status,
			Num:              acc.Num,
			People:           acc.People,
			Price:            acc.Price,
			NumBed:           acc.NumBed,
			NumTolet:         acc.NumTolet,
			Province:         acc.Province,
			District:         acc.District,
			Ward:             acc.Ward,
			Benefits:         acc.Benefits,
			Longitude:        acc.Longitude,
			Latitude:         acc.Latitude,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy danh sách chỗ ở thành công",
		"data": accommodationsResponse,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": total,
		},
	})
}

// Hàm kiểm tra benefit
func normalizeBenefitName(name string) string {
	name = strings.ToLower(name)
	fields := strings.Fields(name)
	normalized := strings.Join(fields, " ")
	return normalized
}

func CreateAccommodation(c *gin.Context) {
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
	var newAccommodation models.Accommodation
	var user models.User
	if err := config.DB.First(&user, currentUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Người dùng không tồn tại"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi kiểm tra người dùng", "details": err.Error()})
		return
	}
	newAccommodation.UserID = currentUserID
	newAccommodation.User = user
	if err := c.ShouldBindJSON(&newAccommodation); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu đầu vào không hợp lệ", "details": err.Error()})
		return
	}

	if err := newAccommodation.ValidateType(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	if err := newAccommodation.ValidateStatus(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	imgJSON, err := json.Marshal(newAccommodation.Img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể mã hóa hình ảnh", "details": err.Error()})
		return
	}

	newAccommodation.Img = imgJSON

	var benefits []models.Benefit

	for _, benefit := range newAccommodation.Benefits {
		if benefit.Id != 0 {

			benefits = append(benefits, benefit)
		} else {

			normalizedBenefitName := normalizeBenefitName(benefit.Name)

			newBenefit := models.Benefit{Name: normalizedBenefitName}
			if err := config.DB.Where("LOWER(TRIM(name)) = ?", normalizedBenefitName).FirstOrCreate(&newBenefit).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "message": "Không thể tạo mới tiện ích", "details": err.Error()})
				return
			}

			benefits = append(benefits, newBenefit)
		}
	}

	newAccommodation.Benefits = benefits

	longitude, latitude, err := services.GetCoordinatesFromAddress(
		newAccommodation.Address,
		newAccommodation.District,
		newAccommodation.Province,
		newAccommodation.Ward,
		os.Getenv("MAPBOX_KEY"),
	)

	newAccommodation.Longitude = longitude
	newAccommodation.Latitude = latitude

	if err := config.DB.Create(&newAccommodation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo chỗ ở", "details": err.Error()})
		return
	}
	// Xử lý Redis cache
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch currentUserRole {
		case 1: // Super Admin
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
		case 2: // Admin
			// Xóa cache của admin
			adminCacheKey := fmt.Sprintf("accommodations:admin:%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			var receptionistIDs []int
			if err := config.DB.Model(&models.User{}).Where("admin_id = ?", currentUserID).Pluck("id", &receptionistIDs).Error; err == nil {
				for _, receptionistID := range receptionistIDs {
					receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", receptionistID)
					_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				}
			}
		case 3: // Receptionist
			var adminID int
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
			if err := config.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err == nil {
				receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", currentUserID)
				adminCacheKey := fmt.Sprintf("accommodations:admin:%d", adminID)
				_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			}
		}
	}
	response := AccommodationDetailResponse{
		ID:               newAccommodation.ID,
		Type:             newAccommodation.Type,
		Name:             newAccommodation.Name,
		Address:          newAccommodation.Address,
		CreateAt:         newAccommodation.CreateAt,
		UpdateAt:         newAccommodation.UpdateAt,
		Avatar:           newAccommodation.Avatar,
		ShortDescription: newAccommodation.ShortDescription,
		Status:           newAccommodation.Status,
		Num:              newAccommodation.Num,
		Furniture:        newAccommodation.Furniture,
		People:           newAccommodation.People,
		Price:            newAccommodation.Price,
		NumBed:           newAccommodation.NumBed,
		NumTolet:         newAccommodation.NumTolet,
		Benefits:         newAccommodation.Benefits,
		TimeCheckIn:      newAccommodation.TimeCheckIn,
		TimeCheckOut:     newAccommodation.TimeCheckOut,
		Province:         newAccommodation.Province,
		District:         newAccommodation.District,
		Ward:             newAccommodation.Ward,
		Longitude:        newAccommodation.Longitude,
		Latitude:         newAccommodation.Latitude,

		User: Actor{
			Name:        user.Name,
			Email:       user.Email,
			PhoneNumber: user.PhoneNumber,
		},
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Tạo chỗ ở thành công", "data": response})
}

func GetAccommodationDetail(c *gin.Context) {
	accommodationId := c.Param("id")

	// Kết nối Redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	// Key cache cho tất cả accommodations
	cacheKey := "allaccommodations:all"

	// Lấy danh sách accommodations từ cache
	var cachedAccommodations []models.Accommodation
	if err := services.GetFromRedis(config.Ctx, rdb, cacheKey, &cachedAccommodations); err == nil {
		for _, acc := range cachedAccommodations {
			if fmt.Sprintf("%d", acc.ID) == accommodationId {
				// Tạo response từ cache
				response := AccommodationDetailResponse{
					ID:               acc.ID,
					Type:             acc.Type,
					Name:             acc.Name,
					Address:          acc.Address,
					CreateAt:         acc.CreateAt,
					UpdateAt:         acc.UpdateAt,
					Avatar:           acc.Avatar,
					Img:              acc.Img,
					ShortDescription: acc.ShortDescription,
					Description:      acc.Description,
					Status:           acc.Status,
					Num:              acc.Num,
					People:           acc.People,
					Price:            acc.Price,
					NumBed:           acc.NumBed,
					NumTolet:         acc.NumTolet,
					Furniture:        acc.Furniture,
					Benefits:         acc.Benefits,
					TimeCheckIn:      acc.TimeCheckIn,
					TimeCheckOut:     acc.TimeCheckOut,
					Province:         acc.Province,
					District:         acc.District,
					Ward:             acc.Ward,
					Longitude:        acc.Longitude,
					Latitude:         acc.Latitude,
					User: Actor{
						Name:          acc.User.Name,
						Email:         acc.User.Email,
						PhoneNumber:   acc.User.PhoneNumber,
						BankShortName: acc.User.Banks[0].BankShortName,
						AccountNumber: acc.User.Banks[0].AccountNumber,
						BankName:      acc.User.Banks[0].BankName,
					},
				}
				c.JSON(http.StatusOK, gin.H{
					"code": 1,
					"mess": "Lấy thông tin chỗ ở thành công (từ cache)",
					"data": response,
				})
				return
			}
		}
	}

	// Nếu không tìm thấy trong cache, truy vấn từ database
	var accommodation models.Accommodation
	if err := config.DB.Preload("Rooms").
		Preload("Rates").
		Preload("Benefits").
		Preload("User").Preload("User.Banks").First(&accommodation, accommodationId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Chỗ ở không tồn tại"})
		return
	}

	response := AccommodationDetailResponse{
		ID:               accommodation.ID,
		Type:             accommodation.Type,
		Name:             accommodation.Name,
		Address:          accommodation.Address,
		CreateAt:         accommodation.CreateAt,
		UpdateAt:         accommodation.UpdateAt,
		Avatar:           accommodation.Avatar,
		Img:              accommodation.Img,
		ShortDescription: accommodation.ShortDescription,
		Description:      accommodation.Description,
		Status:           accommodation.Status,
		Num:              accommodation.Num,
		People:           accommodation.People,
		Price:            accommodation.Price,
		NumBed:           accommodation.NumBed,
		NumTolet:         accommodation.NumTolet,
		Furniture:        accommodation.Furniture,
		Benefits:         accommodation.Benefits,
		TimeCheckIn:      accommodation.TimeCheckIn,
		TimeCheckOut:     accommodation.TimeCheckOut,
		Province:         accommodation.Province,
		District:         accommodation.District,
		Ward:             accommodation.Ward,
		Longitude:        accommodation.Longitude,
		Latitude:         accommodation.Latitude,
		User: Actor{
			Name:          accommodation.User.Name,
			Email:         accommodation.User.Email,
			PhoneNumber:   accommodation.User.PhoneNumber,
			BankShortName: accommodation.User.Banks[0].BankShortName,
			AccountNumber: accommodation.User.Banks[0].AccountNumber,
			BankName:      accommodation.User.Banks[0].BankName,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy thông tin chỗ ở thành công",
		"data": response,
	})
}

func UpdateAccommodation(c *gin.Context) {
	var request AccommodationRequest
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
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu đầu vào không hợp lệ", "details": err.Error()})
		return
	}

	var accommodation models.Accommodation

	if err := config.DB.Preload("User").Preload("Rooms").Preload("Rates").First(&accommodation, request.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Chỗ ở không tồn tại"})
		return
	}

	// Xử lý trường Img
	imgJSON, err := json.Marshal(request.Img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể mã hóa hình ảnh", "details": err.Error()})
		return
	}

	// Xử lý trường Furniture
	furnitureJson, err := json.Marshal(request.Furniture)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể mã hóa nội thất", "details": err.Error()})
		return
	}
	longitude, latitude, err := services.GetCoordinatesFromAddress(
		request.Address,
		request.District,
		request.Province,
		request.Ward,
		os.Getenv("MAPBOX_KEY"),
	)

	if request.Type != -1 {
		accommodation.Type = request.Type
	}

	if request.Name != "" {
		accommodation.Name = request.Name
	}

	if request.Address != "" {
		accommodation.Address = request.Address
	}

	if request.Avatar != "" {
		accommodation.Avatar = request.Avatar
	}

	if request.ShortDescription != "" {
		accommodation.ShortDescription = request.ShortDescription
	}

	if request.Description != "" {
		accommodation.Description = request.Description
	}

	if request.Status != 0 {
		accommodation.Status = request.Status
	}

	if len(request.Img) > 0 {
		accommodation.Img = imgJSON
	}

	if len(request.Furniture) > 0 {
		accommodation.Furniture = furnitureJson
	}

	if request.People != 0 {
		accommodation.People = request.People
	}

	if request.TimeCheckIn != "" {
		accommodation.TimeCheckIn = request.TimeCheckIn
	}

	if request.TimeCheckOut != "" {
		accommodation.TimeCheckOut = request.TimeCheckOut
	}

	if request.Province != "" {
		accommodation.Province = request.Province
	}

	if request.District != "" {
		accommodation.District = request.District
	}

	if request.Ward != "" {
		accommodation.Ward = request.Ward
	}

	if longitude != 0 && latitude != 0 {
		accommodation.Longitude = longitude
		accommodation.Latitude = latitude
	}
	var benefits []models.Benefit
	for _, benefit := range request.Benefits {
		if benefit.Id != 0 {
			benefits = append(benefits, benefit)
		} else {

			newBenefit := models.Benefit{Name: benefit.Name}
			if err := config.DB.Where("name = ?", benefit.Name).FirstOrCreate(&newBenefit).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "message": "Không thể tạo mới tiện ích", "details": err.Error()})
				return
			}
			benefits = append(benefits, newBenefit)
		}
	}

	if err := config.DB.Model(&accommodation).Association("Benefits").Replace(benefits); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật tiện ích", "details": err.Error()})
		return
	}

	if err := config.DB.Save(&accommodation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật chỗ ở", "details": err.Error()})
		return
	}
	// Xử lý Redis cache
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch currentUserRole {
		case 1: // Super Admin
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
			_ = services.DeleteFromRedis(config.Ctx, rdb, "benefits:all")
		case 2: // Admin
			// Xóa cache của admin
			adminCacheKey := fmt.Sprintf("accommodations:admin:%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
			_ = services.DeleteFromRedis(config.Ctx, rdb, "benefits:all")
			var receptionistIDs []int
			if err := config.DB.Model(&models.User{}).Where("admin_id = ?", currentUserID).Pluck("id", &receptionistIDs).Error; err == nil {
				for _, receptionistID := range receptionistIDs {
					receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", receptionistID)
					_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				}
			}
		case 3: // Receptionist
			var adminID int
			_ = services.DeleteFromRedis(config.Ctx, rdb, "benefits:all")
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
			if err := config.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err == nil {
				receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", currentUserID)
				adminCacheKey := fmt.Sprintf("accommodations:admin:%d", adminID)
				_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			}

		}
	}
	response := AccommodationDetailResponse{
		ID:               accommodation.ID,
		Type:             accommodation.Type,
		Name:             accommodation.Name,
		Address:          accommodation.Address,
		CreateAt:         accommodation.CreateAt,
		UpdateAt:         accommodation.UpdateAt,
		Avatar:           accommodation.Avatar,
		Img:              accommodation.Img,
		ShortDescription: accommodation.ShortDescription,
		Description:      accommodation.Description,
		Status:           accommodation.Status,
		Num:              accommodation.Num,
		Furniture:        accommodation.Furniture,
		People:           accommodation.People,
		NumBed:           accommodation.NumBed,
		NumTolet:         accommodation.NumTolet,
		Benefits:         benefits,
		TimeCheckIn:      accommodation.TimeCheckIn,
		TimeCheckOut:     accommodation.TimeCheckOut,
		Longitude:        accommodation.Longitude,
		Latitude:         accommodation.Latitude,
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật chỗ ở thành công", "data": response})
}

func ChangeAccommodationStatus(c *gin.Context) {
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

	var input struct {
		ID     uint `json:"id"`
		Status int  `json:"status"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu đầu vào không hợp lệ"})
		return
	}

	var accommodation models.Accommodation

	if err := config.DB.First(&accommodation, input.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Chỗ ở không tồn tại"})
		return
	}

	accommodation.Status = input.Status
	if err := config.DB.Save(&accommodation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể thay đổi trạng thái chỗ ở"})
		return
	}
	// Xử lý Redis cache
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch currentUserRole {
		case 1: // Super Admin
			_ = services.DeleteFromRedis(config.Ctx, rdb, "accommodations:all")
		case 2: // Admin
			// Xóa cache của admin
			adminCacheKey := fmt.Sprintf("accommodations:admin:%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			var receptionistIDs []int
			if err := config.DB.Model(&models.User{}).Where("admin_id = ?", currentUserID).Pluck("id", &receptionistIDs).Error; err == nil {
				for _, receptionistID := range receptionistIDs {
					receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", receptionistID)
					_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				}
			}
		case 3: // Receptionist
			var adminID int
			if err := config.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err == nil {
				receptionistCacheKey := fmt.Sprintf("accommodations:receptionist:%d", currentUserID)
				adminCacheKey := fmt.Sprintf("accommodations:admin:%d", adminID)
				_ = services.DeleteFromRedis(config.Ctx, rdb, receptionistCacheKey)
				_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Thay đổi trạng thái chỗ ở thành công", "data": accommodation})
}
