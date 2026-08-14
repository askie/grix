package service

import "errors"

var (
	ErrQRLoginCreateFailed         = errors.New("创建二维码失败")
	ErrQRLoginInvalidCode          = errors.New("二维码无效")
	ErrQRLoginExpired              = errors.New("二维码已过期")
	ErrQRLoginAlreadyConfirmed     = errors.New("二维码已确认")
	ErrQRLoginNotReady             = errors.New("二维码尚未确认")
	ErrQRLoginAlreadyConsumed      = errors.New("二维码已使用")
	ErrQRLoginCanceled             = errors.New("二维码已取消")
	ErrQRLoginForbidden            = errors.New("无权操作该二维码")
	ErrQRLoginAlreadyScannedByPeer = errors.New("二维码已被其他账号扫码")
	ErrQRLoginRegionMismatch       = errors.New("二维码属于其他区域的网页，请核对网页地址后重试")
)
