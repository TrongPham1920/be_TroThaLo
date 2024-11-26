package main

import (
	"fmt"
	"new/config"
	_ "new/docs"
	"new/routes"

	"github.com/gin-contrib/cors"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func recreateUserTable() {

	// if err := config.DB.Migrator().DropTable(&models.Rate{}); err != nil {
	// 	panic("Failed to drop Room table: " + err.Error())
	// }

	//if err := config.DB.AutoMigrate(&models.Room{}, &models.Benefit{}, &models.User{}, models.Rate{}, models.Order{}, models.Invoice{}, models.Bank{}, models.Accommodation{}, models.AccommodationStatus{}, models.BankFake{}, models.UserDiscount{}, models.Discount{}, models.Holiday{}, models.RoomStatus{}); err != nil {
	//	panic("Failed to migrate tables: " + err.Error())
	//}

	//if err := config.DB.AutoMigrate(&models.User{}); err != nil {
	//	panic("Failed to migrate tables: " + err.Error())
	//}

	// Thêm cột PaymentType vào bảng Invoice
	// if err := config.DB.Migrator().AddColumn(&models.Invoice{}, "PaymentType"); err != nil {
	// 	log.Fatalf("Failed to add column: %v", err)
	// }

	// Thêm cột PaymentType vào bảng Invoice
	// if err := config.DB.Migrator().AlterColumn(&models.Accommodation{}, "Longitude"); err != nil {
	// 	log.Fatalf("Failed to add column: %v", err)
	// }

	// if err := config.DB.Migrator().AlterColumn(&models.Accommodation{}, "Latitude"); err != nil {
	// 	log.Fatalf("Failed to add column: %v", err)
	// }

	println("User and Bank tables have been recreated successfully.")
}

func main() {
	router := gin.Default()

	err := config.LoadEnv()
	if err != nil {
		panic("Failed to load .env file")
	}

	config.ConnectDB()

	// Khởi tạo Cloudinary
	config.ConnectCloudinary()

	recreateUserTable()

	redisCli, err := config.ConnectRedis()
	if err != nil {
		panic("Failed to connect to Redis!")
	}

	configCors := cors.DefaultConfig()
	configCors.AddAllowHeaders("Authorization")
	configCors.AllowCredentials = true
	configCors.AllowAllOrigins = false
	configCors.AllowOriginFunc = func(origin string) bool {
		return true
	}

	router.Use(cors.New(configCors))

	routes.SetupRoutes(router, config.DB, redisCli, config.Cloudinary)

	router.Use(func(c *gin.Context) {
		c.Next()
		for key, value := range c.Writer.Header() {
			fmt.Println(key, value)
		}
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.Run(":8083")
}
