package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetFavoriteSessionIDs returns all favorited session IDs for the authenticated user.
// Used by the client to bulk-load favorite state on startup.
func GetFavoriteSessionIDs(c *gin.Context) {
	userID := middleware.GetUserID(c)
	ids, err := service.GetFavoriteSessionIDs(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"session_ids": ids})
}

// ListFavoriteSessions returns the user's favorited sessions with full title resolution.
func ListFavoriteSessions(c *gin.Context) {
	userID := middleware.GetUserID(c)

	limit := 50
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	data, err := service.ListFavoriteSessions(userID, limit, offset)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

// GetSessionFavoriteStatus checks whether a specific session is favorited.
func GetSessionFavoriteStatus(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("id")
	data, err := service.GetSessionFavoriteStatus(userID, sessionID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

// AddSessionFavorite adds a session to the user's favorites.
func AddSessionFavorite(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("id")
	err := service.AddSessionFavorite(userID, sessionID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionFavoriteAlreadyExists):
			response.Fail(c, http.StatusConflict, 10020, err.Error())
		case errors.Is(err, service.ErrSessionFavoriteNotMember):
			response.Fail(c, http.StatusForbidden, 40301, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, gin.H{})
}

// RemoveSessionFavorite removes a session from the user's favorites.
func RemoveSessionFavorite(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("id")
	err := service.RemoveSessionFavorite(userID, sessionID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionFavoriteNotFound):
			response.Fail(c, http.StatusNotFound, 10404, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, gin.H{})
}
