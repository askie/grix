package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// ChatStateList serves the watch companion's single read: every chat_states row
// the caller owns. `?state=waiting` narrows it to the sessions blocked on the
// owner (waiting_approval / waiting_question).
func ChatStateList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	waitingOnly := false
	switch c.Query("state") {
	case "":
	case "waiting":
		waitingOnly = true
	default:
		response.Fail(c, http.StatusBadRequest, 10003, "state must be empty or 'waiting'")
		return
	}
	list, err := service.ChatStateList(c.Request.Context(), userID, waitingOnly)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10005, "query chat states failed")
		return
	}
	response.OK(c, gin.H{"list": list})
}
