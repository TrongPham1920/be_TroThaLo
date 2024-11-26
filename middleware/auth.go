package middlewares

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"new/controllers"
	"strings"
)

func AuthMiddleware(requiredRoles ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Authorization header is missing"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		currentUserID, currentUserRole, err := controllers.GetUserIDFromToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "mess": "Invalid token"})
			c.Abort()
			return
		}

		hasRole := false
		for _, role := range requiredRoles {
			if currentUserRole == role {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{"message": "Bạn không có quyền truy cập"})
			c.Abort()
			return
		}

		c.Set("currentUserID", currentUserID)
		c.Set("currentUserRole", currentUserRole)
		c.Next()
	}
}
