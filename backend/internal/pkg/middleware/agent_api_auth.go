package middleware

import (
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

// AgentAPIAuth validates Agent API requests using Bearer api_key authentication.
// On success, sets "agent_id" and "owner_id" in the Gin context.
func AgentAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			response.Fail(c, http.StatusUnauthorized, 10001, "missing or invalid authorization header")
			c.Abort()
			return
		}
		apiKey := strings.TrimPrefix(auth, "Bearer ")
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			response.Fail(c, http.StatusUnauthorized, 10001, "empty api key")
			c.Abort()
			return
		}

		keyHash := agentapi.HashAPIKey(apiKey)
		var agent model.Agent
		if err := store.DB.Where("api_key_hash = ?", keyHash).First(&agent).Error; err != nil {
			response.Fail(c, http.StatusUnauthorized, 10001, "invalid api key")
			c.Abort()
			return
		}

		if agent.ProviderType != model.AgentProviderAPI {
			response.Fail(c, http.StatusForbidden, 10002, "agent is not an API provider")
			c.Abort()
			return
		}
		if agent.Status != model.AgentStatusActive {
			response.Fail(c, http.StatusForbidden, 10002, "agent is not active")
			c.Abort()
			return
		}

		c.Set("agent_id", agent.ID)
		c.Set("owner_id", agent.OwnerID)
		c.Next()
	}
}

// GetAgentID extracts agent_id from the Gin context (set by AgentAPIAuth).
func GetAgentID(c *gin.Context) int64 {
	v, _ := c.Get("agent_id")
	id, _ := v.(int64)
	return id
}

// GetOwnerID extracts owner_id from the Gin context (set by AgentAPIAuth).
func GetOwnerID(c *gin.Context) int64 {
	v, _ := c.Get("owner_id")
	id, _ := v.(int64)
	return id
}
