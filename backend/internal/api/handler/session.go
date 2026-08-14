package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func parseMemberIDs(rawMemberIDs []string) ([]int64, error) {
	memberIDs := make([]int64, 0, len(rawMemberIDs))
	for _, mid := range rawMemberIDs {
		v, err := strconv.ParseInt(strings.TrimSpace(mid), 10, 64)
		if err != nil {
			return nil, service.ErrInvalidMemberID
		}
		memberIDs = append(memberIDs, v)
	}
	return memberIDs, nil
}

func handleSessionServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSessionPermissionDenied):
		response.Fail(c, http.StatusForbidden, 4003, err.Error())
	case errors.Is(err, service.ErrSessionRoleDenied):
		response.Fail(c, http.StatusForbidden, 4003, err.Error())
	case errors.Is(err, service.ErrSessionGroupBanned):
		response.Fail(c, http.StatusForbidden, 4003, err.Error())
	case errors.Is(err, service.ErrSessionOwnerRequired),
		errors.Is(err, service.ErrSessionDissolveDenied),
		errors.Is(err, service.ErrSessionRemoveDenied),
		errors.Is(err, service.ErrSessionMemberSettingDenied),
		errors.Is(err, service.ErrSessionRuntimeSettingDenied),
		errors.Is(err, service.ErrSessionSpeakingTargetDenied):
		response.Fail(c, http.StatusForbidden, 4003, err.Error())
	case errors.Is(err, service.ErrSessionMemberInviteDisabled):
		response.Fail(c, http.StatusForbidden, 40031, err.Error())
	case errors.Is(err, service.ErrSessionMemberInviteThresholdReached):
		response.Fail(c, http.StatusForbidden, 40032, err.Error())
	case errors.Is(err, service.ErrSessionTargetGroupInviteRejected):
		response.Fail(c, http.StatusForbidden, 40033, err.Error())
	case errors.Is(err, service.ErrSessionNotFound):
		response.Fail(c, http.StatusNotFound, 4004, err.Error())
	case errors.Is(err, service.ErrSessionMemberNotFound):
		response.Fail(c, http.StatusNotFound, 4004, err.Error())
	case errors.Is(err, service.ErrSessionOwnerCannotLeave):
		response.Fail(c, http.StatusForbidden, 4003, err.Error())
	case errors.Is(err, service.ErrSessionInvalidType),
		errors.Is(err, service.ErrSessionInvalidRole),
		errors.Is(err, service.ErrSessionCannotRemoveOwner),
		errors.Is(err, service.ErrSessionCannotOperateSelf),
		errors.Is(err, service.ErrSessionTitleTooLong),
		errors.Is(err, service.ErrSessionGroupNicknameTooLong),
		errors.Is(err, service.ErrSessionAgentReceiveModeInvalid),
		errors.Is(err, service.ErrSessionAgentReceiveBacklogInvalid),
		errors.Is(err, service.ErrSessionSpeakingSettingRequired),
		errors.Is(err, service.ErrSessionOwnerSpeakingImmutable),
		errors.Is(err, service.ErrInvalidMemberID),
		errors.Is(err, service.ErrInvalidMemberType),
		errors.Is(err, service.ErrMemberTypesMismatch),
		errors.Is(err, service.ErrMemberUserNotFound),
		errors.Is(err, service.ErrMemberNotFriend),
		errors.Is(err, service.ErrMemberAgentNotFound),
		errors.Is(err, service.ErrMemberAgentNotOwned),
		errors.Is(err, service.ErrMemberAgentUnavailable),
		errors.Is(err, service.ErrMemberAgentVoiceNotAllowed):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	}
}
