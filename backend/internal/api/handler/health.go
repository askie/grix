package handler

import (
	"net/http"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
	"github.com/gin-gonic/gin"
)

func Health(c *gin.Context) {
	c.String(200, "ok")
}

func Version(c *gin.Context) {
	c.JSON(http.StatusOK, version.Get())
}

func Ready(c *gin.Context) {
	dbOk, redisOk, natsOk := store.ReadyCheck(3 * time.Second)
	if dbOk && redisOk {
		c.String(200, "ok")
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"db":    dbOk,
		"redis": redisOk,
		"nats":  natsOk,
	})
}
