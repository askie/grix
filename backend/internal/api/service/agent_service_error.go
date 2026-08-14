package service

import (
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

func internalAgentErr(msg string, err error) *errcode.ErrCode {
	if logger.L != nil {
		logger.L.Errorf("%s: %v", msg, err)
	}
	return &errcode.ErrCode{
		HTTPStatus: 500,
		BizCode:    50001,
		Msg:        msg,
	}
}
