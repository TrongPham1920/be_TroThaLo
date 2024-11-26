package controllers

import (
	"fmt"
	"log"
	"net/http"
	"new/config"
	"sort"
	"strconv"
	"strings"
	"time"

	"new/models"
	"new/services"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type UserController struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func NewUserController(mySQL *gorm.DB, redisCli *redis.Client) UserController {
	return UserController{
		DB:    mySQL,
		Redis: redisCli,
	}
}

type CreateUserRequest struct {
	Email         string `json:"email" binding:"required,email"`
	Password      string `json:"password" binding:"required"`
	PhoneNumber   string `json:"phoneNumber" binding:"required"`
	Role          int    `json:"role"`
	BankId        int    `json:"bankId"`
	AccountNumber string `json:"accountNumber"`
}

type UpdateUser struct {
	Name        string `json:"name"`
	PhoneNumber string `json:"phoneNumber"`
	Avatar      string `json:"avatar"`
	DateOfBirth string `json:"dateOfBirth"`
	Gender      int    `json:"gender"`
}

type StausUser struct {
	Status int  `json:"status"`
	Id     uint `json:"id" binding:"required"`
}

// GetUsers godoc
// @Summary Lấy danh sách người dùng
// @Description Lấy tất cả người dùng cùng với thông tin ngân hàng và con cái của họ.
// @Tags users
// @Produce json
// @Success 200 {object} gin.H {"code": 1, "mess": "Lấy danh sách người dùng thành công", "data": []UserResponse{}}
// @Failure 500 {object} gin.H {"code": 0, "mess": "Thông báo lỗi"}
// @Router /users [get]
// @Description Get a list of all users
// @Produce json
// @Success 200 {array} models.User
// @Router /users [get]
func (u UserController) GetUsers(c *gin.Context) {
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

	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	statusStr := c.Query("status")
	name := c.Query("name")
	roleStr := c.Query("role")

	page := 0
	limit := 10

	if pageStr != "" {
		page, _ = strconv.Atoi(pageStr)
	}
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	// Tạo cache key dựa trên vai trò và bộ lọc
	var cacheKey string
	if currentUserRole == 1 {
		cacheKey = "users:all"
	} else if currentUserRole == 2 {
		cacheKey = fmt.Sprintf("users:role_3:admin_%d", currentUserID)
	} else {
		c.JSON(http.StatusForbidden, gin.H{"code": 0, "mess": "Bạn không có quyền truy cập danh sách này"})
		return
	}

	// Kết nối Redis
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	var allUsers []models.User

	// Kiểm tra cache
	if err := services.GetFromRedis(config.Ctx, rdb, cacheKey, &allUsers); err != nil || len(allUsers) == 0 {
		// Nếu không có dữ liệu trong cache, truy vấn từ DB
		query := u.DB.Preload("Banks").Preload("Children").
			Where("name LIKE ? OR email LIKE ? OR phone_number LIKE ?", "%"+name+"%", "%"+name+"%", "%"+name+"%")

		if roleStr != "" {
			userRole, err := strconv.Atoi(roleStr)
			if err == nil {
				query = query.Where("role = ?", userRole)
			}
		}

		if statusStr != "" {
			status, err := strconv.Atoi(statusStr)
			if err == nil {
				query = query.Where("status = ?", status)
			}
		}

		if currentUserRole == 3 {
			var adminID int
			if err := u.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể xác định admin_id của người dùng hiện tại"})
				return
			}

			query = query.Where("role = 3 AND admin_id = ?", adminID)
		} else if currentUserRole == 2 {
			query = query.Where("role = 3 AND admin_id = ?", currentUserID)
		}

		if err := query.Find(&allUsers).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi lấy danh sách người dùng"})
			return
		}

		// Lưu dữ liệu vào Redis
		if err := services.SetToRedis(config.Ctx, rdb, cacheKey, allUsers, time.Hour); err != nil {
			log.Printf("Lỗi khi lưu danh sách người dùng vào Redis: %v", err)
		}
	}

	// Lọc và chuẩn bị response
	var userResponses []UserResponse
	for _, user := range allUsers {
		if user.ID == uint(currentUserID) {
			continue
		}

		if currentUserRole == 2 {
			if user.Role != 3 || user.AdminId == nil || *user.AdminId != uint(currentUserID) {
				continue
			}
		}

		var banks []Bank
		for _, bank := range user.Banks {
			banks = append(banks, Bank{
				BankName:      bank.BankName,
				AccountNumber: bank.AccountNumber,
				BankShortName: bank.BankShortName,
			})
		}

		var childrenResponses []UserResponse
		for _, child := range user.Children {
			var childBanks []Bank
			for _, bank := range child.Banks {
				childBanks = append(childBanks, Bank{
					BankName:      bank.BankName,
					AccountNumber: bank.AccountNumber,
					BankShortName: bank.BankShortName,
				})
			}

			childrenResponses = append(childrenResponses, UserResponse{
				UserID:       child.ID,
				UserName:     child.Name,
				UserEmail:    child.Email,
				UserVerified: child.IsVerified,
				UserPhone:    child.PhoneNumber,
				UserRole:     child.Role,
				UserAvatar:   child.Avatar,
				UserBanks:    childBanks,
				UserStatus:   child.Status,
				UpdatedAt:    child.UpdatedAt,
				CreatedAt:    child.CreatedAt,
			})
		}

		userResponses = append(userResponses, UserResponse{
			UserID:       user.ID,
			UserName:     user.Name,
			UserEmail:    user.Email,
			UserVerified: user.IsVerified,
			UserPhone:    user.PhoneNumber,
			UserRole:     user.Role,
			UpdatedAt:    user.UpdatedAt,
			CreatedAt:    user.CreatedAt,
			UserAvatar:   user.Avatar,
			UserBanks:    banks,
			UserStatus:   user.Status,
			Children:     childrenResponses,
			AdminId:      user.AdminId,
		})
	}

	// Sắp xếp và phân trang
	sort.Slice(userResponses, func(i, j int) bool {
		return userResponses[i].UserID < userResponses[j].UserID
	})

	start := page * limit
	end := start + limit
	if end > len(userResponses) {
		end = len(userResponses)
	}

	paginatedUsers := userResponses[start:end]

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"mess": "Lấy danh sách người dùng thành công",
		"data": paginatedUsers,
		"pagination": gin.H{
			"page":  page,
			"limit": limit,
			"total": len(userResponses),
		},
	})
}

// CreateUser godoc
// @Summary Tạo người dùng mới
// @Description Tạo một người dùng mới và thông tin ngân hàng nếu có.
// @Tags users
// @Accept json
// @Produce json
// @Param createUserRequest body CreateUserRequest true "Thông tin người dùng cần tạo"
// @Success 201 {object} gin.H {"code": 1, "mess": "Tạo người dùng thành công", "data": models.User}
// @Failure 400 {object} gin.H {"code": 0, "mess": "Thông báo lỗi"}
// @Failure 500 {object} gin.H {"code": 0, "mess": "Không thể tạo ngân hàng"}
// @Router /users/create [post]
func (u UserController) CreateUser(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	currentUserID, err := GetIDFromToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
		return
	}

	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User

	if req.Role == 1 || req.Role == 2 {
		var bankFake models.BankFake
		if err := u.DB.Where("id = ? AND account_numbers::jsonb @> ?", req.BankId, fmt.Sprintf(`["%s"]`, req.AccountNumber)).First(&bankFake).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Không có số tài khoản phù hợp"})
			return
		}

		var existingBank models.Bank
		if err := u.DB.Where("account_number = ?", req.AccountNumber).First(&existingBank).Error; err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Số tài khoản đã có người sử dụng"})
			return
		}

		userValues := models.User{
			Email:       req.Email,
			Password:    req.Password,
			PhoneNumber: req.PhoneNumber,
			Role:        req.Role,
		}

		user, err = services.CreateUser(userValues)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
			return
		}

		bank := models.Bank{
			UserId:        user.ID,
			BankName:      bankFake.BankName,
			AccountNumber: req.AccountNumber,
			BankShortName: bankFake.BankShortName,
		}

		if err := u.DB.Create(&bank).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo ngân hàng: " + err.Error()})
			return
		}

		user.Banks = append(user.Banks, bank)

		if err := u.DB.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật thông tin người dùng: " + err.Error()})
			return
		}
	} else if req.Role == 3 {
		var admin models.User
		if err := u.DB.Where("id = ?", currentUserID).First(&admin).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Không tìm thấy admin với ID: " + fmt.Sprint(currentUserID)})
			return
		}

		userValues := models.User{
			Email:       req.Email,
			Password:    req.Password,
			PhoneNumber: req.PhoneNumber,
			Role:        req.Role,
		}

		user, err = services.CreateUser(userValues)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
			return
		}

		admin.Children = append(admin.Children, user)

		if err := u.DB.Save(&admin).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật thông tin admin: " + err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Vai trò không hợp lệ"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch req.Role {
		case 1, 2:
			_ = services.DeleteFromRedis(config.Ctx, rdb, "user:all")
		case 3:
			adminCacheKey := fmt.Sprintf("users:role_3:admin_%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"code": 1, "mess": "Tạo người dùng thành công", "data": user})
}

// @Summary Get a user by ID
// @Description Get a specific user by ID
// @Produce json
// @Param id path int true "User ID"
// @Success 200 {object} models.User
// @Router /users/{id} [get]
func (u UserController) GetUserByID(c *gin.Context) {
	var user models.User
	id := c.Param("id")

	if err := u.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Người dùng không tồn tại"})
		return
	}

	var banks []Bank
	for _, bank := range user.Banks {
		banks = append(banks, Bank{
			BankName:      bank.BankName,
			AccountNumber: bank.AccountNumber,
			BankShortName: bank.BankShortName,
		})
	}

	userResponse := UserResponse{
		UserID:       user.ID,
		UserName:     user.Name,
		UserEmail:    user.Email,
		UserVerified: user.IsVerified,
		UserPhone:    user.PhoneNumber,
		UserRole:     user.Role,
		UserAvatar:   user.Avatar,
		UserBanks:    banks,
		UserStatus:   user.Status,
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Lấy người dùng thành công", "data": userResponse})
}

// UpdateUser godoc
// @Summary Cập nhật thông tin người dùng
// @Description Cập nhật thông tin của người dùng dựa trên ID của người quản lý.
// @Tags users
// @Accept json
// @Produce json
// @Param updateUser body UpdateUser true "Thông tin người dùng cần cập nhật"
// @Success 200 {object} gin.H {"code": 1, "mess": "Cập nhật người dùng thành công", "data": UserResponse}
// @Failure 400 {object} gin.H {"code": 0, "mess": "Thông báo lỗi"}
// @Failure 404 {object} gin.H {"code": 0, "mess": "Tài khoản admin không tồn tại"}
// @Router /users/update [put]
func (u UserController) UpdateUser(c *gin.Context) {
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

	var updateUser UpdateUser
	if err := c.ShouldBindJSON(&updateUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User
	if err := u.DB.Preload("Banks").First(&user, currentUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Người dùng không tồn tại"})
		return
	}

	if updateUser.Name != "" && updateUser.Name != " " {
		user.Name = updateUser.Name
	}
	if updateUser.PhoneNumber != "" && updateUser.PhoneNumber != " " {
		user.PhoneNumber = updateUser.PhoneNumber
	}
	if updateUser.Avatar != "" && updateUser.Avatar != " " {
		user.Avatar = updateUser.Avatar
	}

	user.Gender = updateUser.Gender

	if updateUser.DateOfBirth != "" && updateUser.DateOfBirth != " " {
		user.DateOfBirth = updateUser.DateOfBirth
	}

	if err := u.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var banks []Bank
	for _, bank := range user.Banks {
		banks = append(banks, Bank{
			BankName:      bank.BankName,
			AccountNumber: bank.AccountNumber,
			BankShortName: bank.BankShortName,
		})
	}

	userResponse := UserResponse{
		UserID:       user.ID,
		UserName:     user.Name,
		UserEmail:    user.Email,
		UserVerified: user.IsVerified,
		UserPhone:    user.PhoneNumber,
		UserRole:     user.Role,
		UserAvatar:   user.Avatar,
		UserBanks:    banks,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		UserStatus:   user.Status,
		AdminId:      user.AdminId,
		Gender:       user.Gender,
		DateOfBirth:  user.DateOfBirth,
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch currentUserRole {
		case 1, 2:
			_ = services.DeleteFromRedis(config.Ctx, rdb, "user:all")
		case 3:
			adminCacheKey := fmt.Sprintf("users:role_3:admin_%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Cập nhật người dùng thành công", "data": userResponse})
}

// ChangeUserStatus godoc
// @Summary Thay đổi trạng thái người dùng
// @Description Thay đổi trạng thái của một người dùng cụ thể dựa trên ID của người quản lý.
// @Tags users
// @Accept json
// @Produce json
// @Param statusRequest body StausUser true "Thông tin trạng thái người dùng"
// @Success 200 {object} gin.H {"code": 1, "mess": "Thay đổi trạng thái người dùng thành công", "data": models.User}
// @Failure 400 {object} gin.H {"code": 0, "mess": "Thông báo lỗi"}
// @Failure 404 {object} gin.H {"code": 0, "mess": "Tài khoản admin không tồn tại"}
// @Failure 403 {object} gin.H {"code": 0, "mess": "Tài khoản này không có quyền cập nhật trạng thái"}
// @Router /users/change-status [post]
func (u UserController) ChangeUserStatus(c *gin.Context) {

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

	var statusRequest StausUser
	if err := c.ShouldBindJSON(&statusRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User

	if currentUserRole == 2 {

		if err := u.DB.Where("id = ? AND admin_id = ?", statusRequest.Id, currentUserID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Người dùng không tồn tại hoặc không thuộc quyền quản lý của admin"})
			return
		}
	} else if currentUserRole == 1 {

		if err := u.DB.First(&user, statusRequest.Id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Người dùng không tồn tại"})
			return
		}

		if user.Role == 2 {
			var childUsers []models.User
			if err := u.DB.Where("admin_id = ?", user.ID).Find(&childUsers).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi tìm tài khoản con"})
				return
			}

			for _, child := range childUsers {
				child.Status = statusRequest.Status
				if err := u.DB.Save(&child).Error; err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi cập nhật trạng thái của tài khoản con"})
					return
				}
			}
		}
	} else {

		c.JSON(http.StatusForbidden, gin.H{"code": 0, "mess": "Tài khoản này không có quyền cập nhật trạng thái"})
		return
	}

	user.Status = statusRequest.Status
	if err := u.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		switch currentUserRole {
		case 1:
			_ = services.DeleteFromRedis(config.Ctx, rdb, "user:all")
		case 3:
			adminCacheKey := fmt.Sprintf("users:role_3:admin_%d", currentUserID)
			_ = services.DeleteFromRedis(config.Ctx, rdb, adminCacheKey)
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Thay đổi trạng thái người dùng thành công", "data": user})
}
