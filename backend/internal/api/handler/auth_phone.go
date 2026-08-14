package handler

import (
	"errors"
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type sendSmsReq struct {
	PhoneE164    string `json:"phone_e164" binding:"required"`
	Scene        string `json:"scene" binding:"required"` // register / login / reset / bind
	CaptchaID    string `json:"captcha_id"`
	CaptchaValue string `json:"captcha_value"`
}

// SendSmsCode POST /v1/auth/sms/send
func SendSmsCode(c *gin.Context) {
	var req sendSmsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	scene, err := parseSmsScene(req.Scene)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	lang := i18n.RequestLanguage(c)
	if err := service.SendPhoneSmsCode(c.ClientIP(), req.PhoneE164, scene, req.CaptchaID, req.CaptchaValue, lang); err != nil {
		// captcha required 单独走 10010，前端据此展开图形验证码输入框；其他错误统一 10005。
		if errors.Is(err, identity.ErrSmsCaptchaRequired) {
			response.Fail(c, http.StatusBadRequest, 10010, err.Error())
			return
		}
		response.Fail(c, http.StatusBadRequest, 10005, err.Error())
		return
	}
	response.OK(c, gin.H{"expires_in": int(identity.SmsCodeTTL.Seconds())})
}

type phoneLoginCodeReq struct {
	PhoneE164 string `json:"phone_e164" binding:"required"`
	Code      string `json:"code" binding:"required"`
	DeviceID  string `json:"device_id" binding:"required"`
	Platform  string `json:"platform" binding:"required"`
}

// PhoneLoginWithCode POST /v1/auth/phone/login-code
//
// 接口幂等：未注册即注册，已注册即登录。返 LoginResp（与现有邮箱登录完全一致）。
func PhoneLoginWithCode(c *gin.Context) {
	var req phoneLoginCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	lang := i18n.RequestLanguage(c)
	data, err := service.PhoneLoginWithCode(req.PhoneE164, req.Code, req.DeviceID, req.Platform, lang, c.ClientIP())
	if err != nil {
		failByLoginError(c, err)
		return
	}
	response.OK(c, data)
}

type bindPhoneReq struct {
	PhoneE164 string `json:"phone_e164" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// BindPhone POST /v1/me/bind-phone（需鉴权）
func BindPhone(c *gin.Context) {
	var req bindPhoneReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := c.GetInt64("user_id")
	if err := service.BindUserPhone(userID, req.PhoneE164, req.Code, c.ClientIP()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10005, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func parseSmsScene(s string) (identity.SmsSendScene, error) {
	switch s {
	case "register":
		return identity.SmsSceneRegister, nil
	case "login":
		return identity.SmsSceneLogin, nil
	case "reset":
		return identity.SmsSceneReset, nil
	case "bind":
		return identity.SmsSceneBind, nil
	}
	return "", errors.New("不支持的 scene")
}
