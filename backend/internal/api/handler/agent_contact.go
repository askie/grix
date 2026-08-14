package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// AgentContactSearch handles GET /v1/agent-api/contacts/search
func AgentContactSearch(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	idText := strings.TrimSpace(c.Query("id"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	var (
		data *service.ContactSearchResp
		err  error
	)
	switch {
	case idText != "":
		id, parseErr := strconv.ParseInt(idText, 10, 64)
		if parseErr != nil {
			response.Fail(c, http.StatusBadRequest, 10003, "id invalid")
			return
		}
		data, err = service.ContactSearchByID(ownerID, id, limit, offset)
	case keyword != "":
		data, err = service.ContactSearch(ownerID, keyword, limit, offset)
	default:
		data, err = service.ContactListAll(ownerID, limit, offset)
	}
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}
