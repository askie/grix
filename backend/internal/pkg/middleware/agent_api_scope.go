package middleware

import (
	"net/http"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

// AgentAPIScope validates whether current agent owns the required scope.
func AgentAPIScope(requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !agentscope.IsAllowed(requiredScope) {
			response.Fail(c, http.StatusInternalServerError, 50001, "invalid route scope config")
			c.Abort()
			return
		}

		agentID := GetAgentID(c)
		if agentID <= 0 {
			response.Fail(c, http.StatusUnauthorized, 10001, "invalid agent context")
			c.Abort()
			return
		}

		var count int64
		if err := store.DB.Model(&model.AgentAPIScope{}).
			Where("agent_id = ? AND scope = ?", agentID, requiredScope).
			Count(&count).Error; err != nil {
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
			c.Abort()
			return
		}
		if count == 0 {
			response.Fail(c, errcode.ErrAgentScopeForbidden.HTTPStatus, errcode.ErrAgentScopeForbidden.BizCode, errcode.ErrAgentScopeForbidden.Msg)
			c.Abort()
			return
		}
		c.Next()
	}
}
