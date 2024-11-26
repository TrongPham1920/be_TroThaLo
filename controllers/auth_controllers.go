package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"new/config"
	"new/models"
	"new/services"
	"os"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
	"gorm.io/gorm"
)

type RegisterInPut struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required"`
	PhoneNumber string `json:"phoneNumber" binding:"required"`
}

type LoginInput struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required"`
}

type Bank struct {
	BankName      string `json:"bankName"`
	AccountNumber string `json:"accountNumber"`
	BankShortName string `json:"bankShortName"`
}

type UserResponse struct {
	UserID       uint           `json:"id"`
	UserName     string         `json:"name"`
	UserEmail    string         `json:"email"`
	UserVerified bool           `json:"verified"`
	UserPhone    string         `json:"phone"`
	UserRole     int            `json:"role"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	UserStatus   int            `json:"status"`
	UserAvatar   string         `json:"avatar"`
	UserBanks    []Bank         `json:"banks"`
	Children     []UserResponse `json:"children,omitempty"`
	AdminId      *uint          `json:"adminId,omitempty"`
	Gender       int            `json:"gender"`
	DateOfBirth  string         `json:"dateOfBirth"`
}

type UserLoginResponse struct {
	UserID       uint      `json:"id"`
	UserName     string    `json:"name"`
	UserEmail    string    `json:"email"`
	UserVerified bool      `json:"verified"`
	UserPhone    string    `json:"phone"`
	UserRole     int       `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	UserStatus   int       `json:"status"`
	UserAvatar   string    `json:"avatar"`
	UserBanks    []Bank    `json:"banks"`
	Gender       int       `json:"gender"`
	DateOfBirth  string    `json:"dateOfBirth"`
}

func Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User
	if err := config.DB.Preload("Banks").Where("email = ? OR phone_number = ?", input.Identifier, input.Identifier).First(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Email hoặc mật khẩu không hợp lệ"})
		return
	}

	if user.Role == 0 {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Email hoặc mật khẩu không hợp lệ"})
			return
		}
	} else {

		if err := services.LoginCode(user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "mess": err.Error()})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Email hoặc mật khẩu không hợp lệ"})
			return
		}
	}

	userInfo := services.UserInfo{
		UserId: user.ID,
		Role:   user.Role,
	}

	accessToken, err := services.GenerateToken(userInfo, 60*24*3, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
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

	userResponse := UserLoginResponse{
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
		Gender:       user.Gender,
		DateOfBirth:  user.DateOfBirth,
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Đăng nhập thành công", "data": gin.H{
		"user_info":   userResponse,
		"accessToken": accessToken,
	}})
}

func Logout(c *gin.Context) {
	cookies := c.Request.Cookies()
	for _, cookie := range cookies {

		c.SetCookie(cookie.Name, "", -1, "/", "", cookie.Secure, cookie.HttpOnly)
	}
	redisClient, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	err = redisClient.FlushDB(config.Ctx).Err()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể xóa dữ liệu Redis"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Đăng xuất thành công"})
}

func VerifyEmail(c *gin.Context) {
	code := c.Query("token")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Cần mã xác thực"})
		return
	}

	var user models.User
	result := config.DB.Where("code = ?", code).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": result.Error.Error()})
		return
	}

	// Kiểm tra xem mã xác thực đã hết hạn chưa (5 phút)
	if time.Since(user.CodeCreatedAt) > 5*time.Minute {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Mã xác thực đã hết hạn. Vui lòng yêu cầu mã mới."})
		return
	}

	user.IsVerified = true
	user.Code = ""
	config.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Email đã được xác thực", "data": user})
}

func RegisterUser(c *gin.Context) {
	var input models.User
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	user, err := services.CreateUser(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Đăng ký thành công!", "data": user})
}

func ResendVerificationCode(c *gin.Context) {
	var input struct {
		Identifier string `json:"identifier" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User
	result := config.DB.Where("email = ? OR phone_number = ?", input.Identifier, input.Identifier).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Người dùng không tồn tại."})
		return
	}

	err := services.RegenerateVerificationCode(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi trong quá trình tạo mã xác thực mới."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Mã xác nhận mới đã được gửi đến email của bạn."})
}

func ForgetPassword(c *gin.Context) {
	var input struct {
		Identifier string `json:"identifier" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User
	result := config.DB.Where("email = ? OR phone_number = ?", input.Identifier, input.Identifier).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Người dùng không tồn tại."})
		return
	}

	err := services.ResetPass(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể gửi mã xác nhận: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Mã xác nhận để đặt lại mật khẩu đã được gửi đến email của bạn."})
}

func ResetPassword(c *gin.Context) {
	var input LoginInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": err.Error()})
		return
	}

	var user models.User
	result := config.DB.Where("email = ? OR phone_number = ?", input.Identifier, input.Identifier).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Người dùng không tồn tại."})
		return
	}

	err := services.NewPass(user, input.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể đổi mật khẩu: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Mật khẩu đổi thành công"})
}

func VerifyCode(c *gin.Context) {
	var input struct {
		Email string `json:"email" binding:"required"`
		Code  string `json:"code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ", "error": err.Error()})
		return
	}

	var user models.User
	result := config.DB.Where("email = ?", input.Email).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Không tìm thấy người dùng với email này"})
		return
	}

	if user.Code != input.Code {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Mã xác thực không hợp lệ"})
		return
	}

	if time.Since(user.CodeCreatedAt) > 5*time.Minute {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Mã xác thực đã hết hạn. Vui lòng yêu cầu mã mới."})
		return
	}

	user.Code = ""
	if !user.IsVerified {
		user.IsVerified = true
	}

	config.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Xác thực thành công"})
}

func GetUserIDFromToken(tokenString string) (uint, int, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return 0, 0, fmt.Errorf("invalid token format")
	}

	// Giải mã phần payload của token
	payload, err := jwt.DecodeSegment(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode token payload: %w", err)
	}

	claimsMap := jwt.MapClaims{}
	if err := json.Unmarshal(payload, &claimsMap); err != nil {
		return 0, 0, fmt.Errorf("failed to unmarshal token payload: %w", err)
	}

	// Trích xuất userID và role từ claims
	userInfo, ok := claimsMap["userinfo"].(map[string]interface{})
	if !ok {
		return 0, 0, fmt.Errorf("userinfo not found in token claims")
	}

	userID, okID := userInfo["userid"].(float64)
	if !okID {
		return 0, 0, fmt.Errorf("user ID not found in userinfo")
	}

	role, okRole := userInfo["role"].(float64)
	if !okRole {
		return 0, 0, fmt.Errorf("role not found in userinfo")
	}

	return uint(userID), int(role), nil // Trả về userID và role
}

func GetIDFromToken(tokenString string) (uint, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid token format")
	}

	payload, err := jwt.DecodeSegment(parts[1])
	if err != nil {
		return 0, fmt.Errorf("failed to decode token payload: %w", err)
	}

	claimsMap := jwt.MapClaims{}
	if err := json.Unmarshal(payload, &claimsMap); err != nil {
		return 0, fmt.Errorf("failed to unmarshal token payload: %w", err)
	}

	userInfo, ok := claimsMap["userinfo"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("userinfo not found in token claims")
	}

	userID, okID := userInfo["userid"].(float64)
	if !okID {
		return 0, fmt.Errorf("user ID not found in userinfo")
	}

	return uint(userID), nil
}

type GoogleUser struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Picture       string `json:"picture"`
}

// AuthGoogle function để xử lý yêu cầu xác thực từ Google
func AuthGoogle(c *gin.Context) {
	var token struct {
		TokenId string `json:"tokenId"`
	}

	// Bind dữ liệu token từ request
	if err := c.ShouldBindJSON(&token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Xác minh tokenId từ Google
	payload, err := verifyGoogleIDToken(token.TokenId)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Google token"})
		return
	}
	// Lấy thông tin người dùng từ payload
	googleUser := GoogleUser{
		Name:          payload.Claims["name"].(string),
		Email:         payload.Claims["email"].(string),
		VerifiedEmail: payload.Claims["email_verified"].(bool),
		Picture:       payload.Claims["picture"].(string),
	}
	// Kiểm tra nếu email chưa được xác thực
	if !googleUser.VerifiedEmail {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email has not been verified"})
		return
	}

	user := models.User{}
	result := config.DB.Where("email = ?", googleUser.Email).First(&user)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		user, err = services.CreateGoogleUser(googleUser.Name, googleUser.Email, googleUser.Picture)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create new user"})
			return
		}
	} else if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query user: " + result.Error.Error()})
		return
	}

	userResponse := UserLoginResponse{
		UserID:       user.ID,
		UserName:     user.Name,
		UserEmail:    user.Email,
		UserVerified: user.IsVerified,
		UserPhone:    user.PhoneNumber,
		UserRole:     user.Role,
		UpdatedAt:    user.UpdatedAt,
		CreatedAt:    user.CreatedAt,
		UserAvatar:   user.Avatar,
		Gender:       user.Gender,
		DateOfBirth:  user.DateOfBirth,
	}
	userInfo := services.UserInfo{
		UserId: user.ID,
		Role:   user.Role,
	}

	accessToken, err := services.GenerateToken(userInfo, 15, true)
	if err != nil {
		log.Println("Error generating access token:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1,
		"message": "Login successful",
		"data": gin.H{
			"user_info":   userResponse,
			"accessToken": accessToken,
		},
	})
}

// verifyGoogleIDToken function - Xác thực ID token từ Google
func verifyGoogleIDToken(tokenId string) (*idtoken.Payload, error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	payload, err := idtoken.Validate(context.Background(), tokenId, clientID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
