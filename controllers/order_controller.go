package controllers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"new/config"
	"new/models"
	"new/services"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type OrderUserResponse struct {
	ID               uint                       `json:"id"`
	User             Actor                      `json:"user"`
	Accommodation    OrderAccommodationResponse `json:"accommodation"`
	Room             []OrderRoomResponse        `json:"room"`
	CheckInDate      string                     `json:"checkInDate"`
	CheckOutDate     string                     `json:"checkOutDate"`
	Status           int                        `json:"status"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
	Price            int                        `json:"price"`            // Giá cơ bản cho mỗi phòng
	HolidayPrice     float64                    `json:"holidayPrice"`     // Giá lễ
	CheckInRushPrice float64                    `json:"checkInRushPrice"` // Giá check-in gấp
	SoldOutPrice     float64                    `json:"soldOutPrice"`     // Giá sold out
	DiscountPrice    float64                    `json:"discountPrice"`    // Giá discount
	TotalPrice       float64                    `json:"totalPrice"`
	InvoiceCode      string                     `json:"invoiceCode"`
}

type OrderAccommodationResponse struct {
	ID      uint   `json:"id"`
	Type    int    `json:"type"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Price   int    `json:"price"`
	Avatar  string `json:"avatar"`
}

type OrderRoomResponse struct {
	ID              uint   `json:"id"`
	AccommodationID uint   `json:"accommodationId"`
	RoomName        string `json:"roomName"`
	Price           int    `json:"price"`
}

type CreateOrderRequest struct {
	UserID          uint   `json:"userId"`
	AccommodationID uint   `json:"accommodationId"`
	RoomID          []uint `json:"roomId"`
	CheckInDate     string `json:"checkInDate"`
	CheckOutDate    string `json:"checkOutDate"`
	GuestName       string `json:"guestName,omitempty"`
	GuestEmail      string `json:"guestEmail,omitempty"`
	GuestPhone      string `json:"guestPhone,omitempty"`
}

func convertToOrderAccommodationResponse(accommodation models.Accommodation) OrderAccommodationResponse {
	return OrderAccommodationResponse{
		ID:      accommodation.ID,
		Type:    accommodation.Type,
		Name:    accommodation.Name,
		Address: accommodation.Address,
		Price:   accommodation.Price,
		Avatar:  accommodation.Avatar,
	}
}

func convertToOrderRoomResponse(room models.Room) OrderRoomResponse {
	return OrderRoomResponse{
		ID:              room.RoomId,
		AccommodationID: room.AccommodationID,
		RoomName:        room.RoomName,
		Price:           room.Price,
	}
}

// Chuyển chuỗi ngày string thành dạng timestamp
func ConvertDateToISOFormat(dateStr string) (time.Time, error) {
	parsedDate, err := time.Parse("02/01/2006", dateStr)
	if err != nil {
		return time.Time{}, err
	}
	return parsedDate, nil
}

func GetOrders(c *gin.Context) {
	// Lấy Authorization Header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
		return
	}

	// Xử lý token
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	currentUserID, currentUserRole, err := GetUserIDFromToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
		return
	}

	// Kết nối Redis
	cacheKey := fmt.Sprintf("orders:all:user:%d", currentUserID)
	rdb, err := config.ConnectRedis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể kết nối Redis"})
		return
	}

	var allOrders []models.Order

	// Lấy dữ liệu từ Redis Cache
	if err := services.GetFromRedis(config.Ctx, rdb, cacheKey, &allOrders); err != nil || len(allOrders) == 0 {
		// Nếu không có cache hoặc Redis gặp lỗi, thực hiện truy vấn từ DB
		baseTx := config.DB.Model(&models.Order{}).
			Preload("Accommodation").
			Preload("Room").
			Preload("User")

		// Áp dụng quyền truy cập
		if currentUserRole == 2 {
			// Admin: Lọc theo các chỗ ở thuộc về Admin
			baseTx = baseTx.Where("orders.accommodation_id IN (?)",
				config.DB.Model(&models.Accommodation{}).Select("id").Where("user_id = ?", currentUserID))
		} else if currentUserRole == 3 {
			// Receptionist: Lọc theo các chỗ ở thuộc về Admin của Receptionist
			var adminID int
			if err := config.DB.Model(&models.User{}).Select("admin_id").Where("id = ?", currentUserID).Scan(&adminID).Error; err != nil || adminID == 0 {
				c.JSON(http.StatusForbidden, gin.H{"code": 0, "mess": "Không có quyền truy cập"})
				return
			}
			baseTx = baseTx.Where("orders.accommodation_id IN (?)",
				config.DB.Model(&models.Accommodation{}).Select("id").Where("user_id = ?", adminID))
		}

		// Truy vấn tất cả đơn hàng từ DB
		if err := baseTx.Find(&allOrders).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy danh sách đơn hàng"})
			return
		}

		// Lưu vào Redis Cache
		if err := services.SetToRedis(config.Ctx, rdb, cacheKey, allOrders, time.Hour); err != nil {
			log.Printf("Lỗi khi lưu danh sách đơn hàng vào Redis: %v", err)
		}
	}

	// Lấy các tham số filter từ query
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	nameFilter := c.Query("name")
	phoneStr := c.Query("phoneNumber")
	priceStr := c.Query("price")
	fromDateStr := c.Query("fromDate")
	toDateStr := c.Query("toDate")

	// Xử lý phân trang
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

	// Áp dụng bộ lọc
	filteredOrders := make([]models.Order, 0)
	for _, order := range allOrders {
		if nameFilter != "" {
			decodedName, _ := url.QueryUnescape(nameFilter)
			if !strings.Contains(strings.ToLower(order.Accommodation.Name), strings.ToLower(decodedName)) {
				continue
			}
		}
		if phoneStr != "" {
			if order.User != nil && !strings.Contains(strings.ToLower(order.User.PhoneNumber), strings.ToLower(phoneStr)) {
				continue
			}
			if order.User == nil && !strings.Contains(strings.ToLower(order.GuestPhone), strings.ToLower(phoneStr)) {
				continue
			}
		}
		if priceStr != "" {
			price, err := strconv.ParseFloat(priceStr, 64)
			if err == nil && order.TotalPrice < price {
				continue
			}
		}
		if fromDateStr != "" {
			fromDateISO, err := ConvertDateToISOFormat(fromDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Sai định dạng fromDate"})
				return
			}
			if order.CreatedAt.Before(fromDateISO) {
				continue
			}
		}
		if toDateStr != "" {
			toDateISO, err := ConvertDateToISOFormat(toDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Sai định dạng toDate"})
				return
			}
			if order.UpdatedAt.After(toDateISO) {
				continue
			}
		}
		filteredOrders = append(filteredOrders, order)
	}

	// Tính toán lại tổng số đơn hàng sau khi lọc
	totalFiltered := len(filteredOrders)

	//Xếp theo update mới nhất
	sort.Slice(filteredOrders, func(i, j int) bool {
		return filteredOrders[i].UpdatedAt.After(filteredOrders[j].UpdatedAt)
	})
	// Áp dụng phân trang
	start := page * limit
	end := start + limit
	if start >= totalFiltered {
		filteredOrders = []models.Order{}
	} else if end > totalFiltered {
		filteredOrders = filteredOrders[start:]
	} else {
		filteredOrders = filteredOrders[start:end]
	}

	// Chuẩn bị phản hồi
	var orderResponses []OrderUserResponse
	for _, order := range filteredOrders {
		var user Actor
		if order.UserID != nil {
			user = Actor{Name: order.User.Name, Email: order.User.Email, PhoneNumber: order.User.PhoneNumber}
		} else {
			user = Actor{Name: order.GuestName, Email: order.GuestEmail, PhoneNumber: order.GuestPhone}
		}

		accommodationResponse := convertToOrderAccommodationResponse(order.Accommodation)
		var roomResponses []OrderRoomResponse
		for _, room := range order.Room {
			roomResponse := convertToOrderRoomResponse(room)
			roomResponses = append(roomResponses, roomResponse)
		}

		orderResponse := OrderUserResponse{
			ID:               order.ID,
			User:             user,
			Accommodation:    accommodationResponse,
			Room:             roomResponses,
			CheckInDate:      order.CheckInDate,
			CheckOutDate:     order.CheckOutDate,
			Status:           order.Status,
			CreatedAt:        order.CreatedAt,
			UpdatedAt:        order.UpdatedAt,
			Price:            order.Price,
			HolidayPrice:     order.HolidayPrice,
			CheckInRushPrice: order.CheckInRushPrice,
			SoldOutPrice:     order.SoldOutPrice,
			DiscountPrice:    order.DiscountPrice,
			TotalPrice:       order.TotalPrice,
		}
		orderResponses = append(orderResponses, orderResponse)
	}

	// Phản hồi kết quả
	c.JSON(http.StatusOK, gin.H{
		"code":       1,
		"mess":       "Lấy danh sách đơn hàng thành công",
		"data":       orderResponses,
		"pagination": gin.H{"page": page, "limit": limit, "total": totalFiltered},
	})
}

func CreateOrder(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")

	var currentUserID uint
	if authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		userID, _, err := GetUserIDFromToken(tokenString)

		if err == nil {
			currentUserID = userID
		}
	}

	var request CreateOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}

	checkInDate, err := time.Parse("02/01/2006", request.CheckInDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày nhận phòng không hợp lệ"})
		return
	}

	if checkInDate.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày nhận phòng không được nhỏ hơn ngày hiện tại"})
		return
	}

	checkOutDate, err := time.Parse("02/01/2006", request.CheckOutDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày trả phòng không hợp lệ"})
		return
	}
	var info models.User
	var userId *uint
	if err := config.DB.Where("phone_number =?", request.GuestPhone).First(&info).Error; err != nil {
		userId = nil
	} else {
		userId = &info.ID
	}
	order := models.Order{
		UserID:          userId,
		AccommodationID: request.AccommodationID,
		RoomID:          request.RoomID,
		CheckInDate:     request.CheckInDate,
		CheckOutDate:    request.CheckOutDate,
		GuestName:       request.GuestName,
		GuestEmail:      request.GuestEmail,
		GuestPhone:      request.GuestPhone,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	numDays := int(checkOutDate.Sub(checkInDate).Hours() / 24)
	if numDays <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Ngày trả phòng phải sau ngày nhận phòng"})
		return
	}

	price := 0
	soldOutPrice := 0.0

	var accommodation models.Accommodation
	if err := config.DB.First(&accommodation, request.AccommodationID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tìm thấy thông tin chỗ ở"})
		return
	}

	if accommodation.Type == 0 && len(order.RoomID) > 0 {
		var rooms []models.Room
		if err := config.DB.Where("room_id IN ?", order.RoomID).Find(&rooms).Error; err != nil || len(rooms) != len(order.RoomID) {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tìm thấy phòng"})
			return
		}

		for _, room := range rooms {
			if room.AccommodationID != request.AccommodationID {
				c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "AccommodationID không hợp lệ"})
				return
			}

			var roomStatus []models.RoomStatus
			err := config.DB.Where("room_id = ? AND status = 1 AND ((from_date < ? AND to_date > ?) OR (from_date < ? AND to_date > ?))",
				room.RoomId, checkOutDate, checkInDate, checkOutDate, checkInDate).Find(&roomStatus).Error

			if err != nil {
				c.JSON(http.StatusCreated, gin.H{"code": 0, "mess": "Lỗi kiểm tra trạng thái phòng"})
				return
			}

			if len(roomStatus) > 0 {
				c.JSON(http.StatusCreated, gin.H{"code": 0, "mess": "Phòng đã được đặt hoặc không khả dụng trong khoảng thời gian này"})
				return
			}
			price += room.Price * numDays

		}
	} else {

		var accommodationStatus []models.AccommodationStatus
		if err := config.DB.Where("accommodation_id = ? AND status = 1 AND ((from_date < ? AND to_date > ?) OR (from_date < ? AND to_date > ?))",
			request.AccommodationID, checkOutDate, checkInDate, checkOutDate, checkInDate).Find(&accommodationStatus).Error; err != nil {
			c.JSON(http.StatusCreated, gin.H{"code": 0, "mess": "Lỗi kiểm tra trạng thái chỗ ở"})
			return
		}

		if len(accommodationStatus) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Chỗ ở đã được đặt hoặc không khả dụng trong khoảng thời gian này"})
			return
		}

		price = accommodation.Price
	}

	order.Price = price
	order.SoldOutPrice = soldOutPrice

	if request.UserID != 0 {
		order.UserID = &request.UserID
		isEligibleForDiscount := services.CheckUserEligibilityForDiscount(request.UserID)
		if isEligibleForDiscount {
			var user models.User
			if err := config.DB.First(&user, request.UserID).Error; err == nil {
				discountPrice, err := services.ApplyDiscountForUser(user)
				if err != nil {
					fmt.Println(err.Error())
					return
				}
				order.DiscountPrice = float64(price) * discountPrice / 100
			} else {
				fmt.Println("Không tìm thấy người dùng")
				return
			}
		}
	}

	var holidays []models.Holiday
	if err := config.DB.Find(&holidays).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể lấy thông tin ngày lễ"})
		return
	}

	holidayPrice := 0
	for _, holiday := range holidays {
		fromDate, err := time.Parse("02/01/2006", holiday.FromDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Ngày bắt đầu kỳ nghỉ không hợp lệ"})
			return
		}

		toDate, err := time.Parse("02/01/2006", holiday.ToDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Ngày kết thúc kỳ nghỉ không hợp lệ"})
			return
		}

		if (checkInDate.Before(toDate) && checkOutDate.After(fromDate)) ||
			checkInDate.Equal(fromDate) ||
			checkOutDate.Equal(toDate) {
			holidayPrice += holiday.Price
		}
	}
	order.HolidayPrice = float64(price*holidayPrice) / 100

	numDaysToCheckIn := int(checkInDate.Sub(order.CreatedAt).Hours() / 24)

	if numDaysToCheckIn <= 3 {
		order.CheckInRushPrice = float64(price*5) / 100
	} else {
		order.CheckInRushPrice = 0
	}

	order.TotalPrice = float64(price) + order.HolidayPrice + order.CheckInRushPrice + order.SoldOutPrice - order.DiscountPrice

	if len(request.RoomID) > 0 {
		order.RoomID = request.RoomID
	} else {
		order.RoomID = []uint{}
	}

	if err := config.DB.Create(&order).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tạo đơn", "detai": err})
		return
	}

	if accommodation.Type == 0 && len(order.RoomID) > 0 {
		var roomsToAppend []models.Room
		for _, roomID := range request.RoomID {
			roomsToAppend = append(roomsToAppend, models.Room{RoomId: roomID})
		}

		if err := config.DB.Model(&order).Association("Room").Append(roomsToAppend); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể liên kết phòng với đơn hàng", "detail": err})
			return
		}

		for _, roomID := range request.RoomID {
			roomStatus := models.RoomStatus{
				RoomID:   roomID,
				Status:   1,
				FromDate: checkInDate,
				ToDate:   checkOutDate,
			}
			if err := config.DB.Create(&roomStatus).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật trạng thái phòng", "detail": err})
				return
			}
		}
	} else {
		roomStatus := models.AccommodationStatus{
			AccommodationID: request.AccommodationID,
			Status:          1,
			FromDate:        checkInDate,
			ToDate:          checkOutDate,
		}
		if err := config.DB.Create(&roomStatus).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể cập nhật trạng thái phòng", "detail": err})
			return
		}
	}

	if err := config.DB.Preload("User").Preload("Accommodation").Preload("Room").First(&order, order.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể tải dữ liệu đơn hàng sau khi tạo"})
		return
	}

	var user Actor
	if order.UserID != nil {
		user = Actor{Name: order.User.Name, Email: order.User.Email, PhoneNumber: order.User.PhoneNumber}
	} else {
		user = Actor{Name: order.GuestName, Email: order.GuestEmail, PhoneNumber: order.GuestPhone}
	}

	accommodationResponse := convertToOrderAccommodationResponse(order.Accommodation)
	var roomResponses []OrderRoomResponse
	if len(request.RoomID) > 0 {
		for _, room := range order.Room {
			roomResponse := convertToOrderRoomResponse(room)
			roomResponses = append(roomResponses, roomResponse)
		}
	}

	orderResponse := OrderUserResponse{
		ID:               order.ID,
		User:             user,
		Accommodation:    accommodationResponse,
		Room:             roomResponses,
		CheckInDate:      order.CheckInDate,
		CheckOutDate:     order.CheckOutDate,
		Status:           order.Status,
		CreatedAt:        order.CreatedAt,
		UpdatedAt:        order.UpdatedAt,
		Price:            price,
		HolidayPrice:     order.HolidayPrice,
		CheckInRushPrice: order.CheckInRushPrice,
		SoldOutPrice:     order.SoldOutPrice,
		DiscountPrice:    order.DiscountPrice,
		TotalPrice:       order.TotalPrice,
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "orders:all"
		cacheKeyUser := fmt.Sprintf("orders:all:user:%d", currentUserID)

		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
		_ = services.DeleteFromRedis(config.Ctx, rdb, "invoices:all")
		_ = services.DeleteFromRedis(config.Ctx, rdb, "total_revenue")
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKeyUser)
	}

	c.JSON(http.StatusCreated, gin.H{"code": 1, "mess": "Tạo đơn thành công", "data": orderResponse})
}

func ChangeOrderStatus(c *gin.Context) {
	type StatusUpdateRequest struct {
		ID         uint    `json:"id"`
		Status     int     `json:"status"`
		PaidAmount float64 `json:"paidAmount"`
	}

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

	var req StatusUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "mess": "Dữ liệu không hợp lệ"})
		return
	}

	var order models.Order
	if err := config.DB.First(&order, req.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "mess": "Đơn hàng không tồn tại"})
		return
	}

	//Kiểm tra nếu người dùng (userRol == 0) đặt đơn chưa quá 24h thì cho Hủy đơn
	if currentUserRole == 0 && req.Status == 2 {
		timeSinceCreation := time.Since(order.CreatedAt)
		if timeSinceCreation.Hours() > 24 {
			c.JSON(http.StatusAccepted, gin.H{"code": 0, "mess": "Liên hệ Admin để được hủy đơn"})
			return
		}
	}

	if req.Status == 2 {
		if len(order.RoomID) > 0 {
			for _, room := range order.Room {
				var roomStatus models.RoomStatus
				if err := config.DB.Where("room_id = ? AND status = ?", room.RoomId, 1).First(&roomStatus).Error; err == nil {
					roomStatus.Status = 0
					if err := config.DB.Save(&roomStatus).Error; err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi cập nhật trạng thái phòng"})
						return
					}
				}
			}
		} else {
			var accommodationStatus models.AccommodationStatus
			if err := config.DB.Where("accommodation_id = ? AND status = ?", order.AccommodationID, 1).First(&accommodationStatus).Error; err == nil {
				accommodationStatus.Status = 0
				if err := config.DB.Save(&accommodationStatus).Error; err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi cập nhật trạng thái accommodation"})
					return
				}
			}
		}
	}

	if req.Status == 1 {
		var Remaining = order.TotalPrice - req.PaidAmount

		invoice := models.Invoice{
			OrderID:         order.ID,
			TotalAmount:     order.TotalPrice,
			PaidAmount:      req.PaidAmount,
			RemainingAmount: Remaining,
		}

		if err := config.DB.Create(&invoice).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi tạo hóa đơn"})
			return
		}
	}

	order.Status = req.Status
	order.UpdatedAt = time.Now()

	if err := config.DB.Save(&order).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Không thể chuyển trạng thái đơn hàng"})
		return
	}

	//Xóa redis
	rdb, redisErr := config.ConnectRedis()
	if redisErr == nil {
		cacheKey := "orders:all"
		cacheKeyUser := fmt.Sprintf("orders:all:user:%d", currentUserID)

		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKey)
		_ = services.DeleteFromRedis(config.Ctx, rdb, "invoices:all")
		_ = services.DeleteFromRedis(config.Ctx, rdb, "total_revenue")
		_ = services.DeleteFromRedis(config.Ctx, rdb, cacheKeyUser)

	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Trạng thái đơn hàng đã được cập nhật"})
}

func GetOrderDetail(c *gin.Context) {
	orderId := c.Param("id")

	var order models.Order
	if err := config.DB.Preload("User").
		Preload("Accommodation").
		Preload("Room").
		Where("id = ?", orderId).
		First(&order).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"code": 0, "error": "Không tìm thấy Order"})
		return
	}
	var user Actor
	if order.UserID != nil {
		user = Actor{Name: order.User.Name, Email: order.User.Email, PhoneNumber: order.User.PhoneNumber}
	} else {
		user = Actor{Name: order.GuestName, Email: order.GuestEmail, PhoneNumber: order.GuestPhone}
	}

	accommodationResponse := convertToOrderAccommodationResponse(order.Accommodation)

	var roomResponses []OrderRoomResponse
	for _, room := range order.Room {
		roomResponse := convertToOrderRoomResponse(room)
		roomResponses = append(roomResponses, roomResponse)
	}
	orderResponse := OrderUserResponse{
		ID:               order.ID,
		User:             user,
		Accommodation:    accommodationResponse,
		Room:             roomResponses,
		CheckInDate:      order.CheckInDate,
		CheckOutDate:     order.CheckOutDate,
		Status:           order.Status,
		CreatedAt:        order.CreatedAt,
		UpdatedAt:        order.UpdatedAt,
		Price:            order.Price,
		HolidayPrice:     order.HolidayPrice,
		CheckInRushPrice: order.CheckInRushPrice,
		SoldOutPrice:     order.SoldOutPrice,
		DiscountPrice:    order.DiscountPrice,
		TotalPrice:       order.TotalPrice,
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": orderResponse})
}

func GetOrdersByUserId(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
		return
	}

	// Xử lý token
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	currentUserID, _, err := GetUserIDFromToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
		return
	}
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

	var totalOrders int64
	if err := config.DB.Model(&models.Order{}).Where("user_id = ?", currentUserID).Count(&totalOrders).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi đếm đơn đặt"})
		return
	}

	var orders []models.Order
	result := config.DB.Preload("User").
		Preload("Accommodation").
		Preload("Room").
		Where("user_id = ?", currentUserID).
		Order("created_at DESC").
		Offset(page * limit).
		Limit(limit).
		Find(&orders)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "mess": "Lỗi khi lấy thông tin đơn đặt!"})
		return
	}
	if len(orders) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Không có đơn đặt nào!", "data": []models.Order{}})
		return
	}

	orderResponses := make([]OrderUserResponse, 0)
	for _, order := range orders {
		var user Actor
		if order.UserID != nil {
			user = Actor{Name: order.User.Name, Email: order.User.Email, PhoneNumber: order.User.PhoneNumber}
		} else {
			user = Actor{Name: order.GuestName, Email: order.GuestEmail, PhoneNumber: order.GuestPhone}
		}

		accommodationResponse := convertToOrderAccommodationResponse(order.Accommodation)
		var roomResponses []OrderRoomResponse
		for _, room := range order.Room {
			roomResponse := convertToOrderRoomResponse(room)
			roomResponses = append(roomResponses, roomResponse)
		}

		var invoiceCode string
		if order.Status == 1 {
			var invoice models.Invoice
			if err := config.DB.Where("order_id = ?", order.ID).First(&invoice).Error; err == nil {
				invoiceCode = invoice.InvoiceCode
			}
		}

		orderResponse := OrderUserResponse{
			ID:               order.ID,
			User:             user,
			Accommodation:    accommodationResponse,
			Room:             roomResponses,
			CheckInDate:      order.CheckInDate,
			CheckOutDate:     order.CheckOutDate,
			Status:           order.Status,
			CreatedAt:        order.CreatedAt,
			UpdatedAt:        order.UpdatedAt,
			Price:            order.Price,
			HolidayPrice:     order.HolidayPrice,
			CheckInRushPrice: order.CheckInRushPrice,
			SoldOutPrice:     order.SoldOutPrice,
			DiscountPrice:    order.DiscountPrice,
			TotalPrice:       order.TotalPrice,
			InvoiceCode:      invoiceCode,
		}
		orderResponses = append(orderResponses, orderResponse)
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "mess": "Orders fetched successfully", "data": orderResponses,
		"page":  page,
		"limit": limit,
		"total": totalOrders,
	})

}
