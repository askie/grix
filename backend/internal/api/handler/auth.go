package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/security"
	"github.com/gin-gonic/gin"
)

// failByLoginError 登录类接口的统一错误语义化映射：
// 账号被禁用 → 403；登录锁定 → 429；基础设施不可用 → 503；其余（凭证错误等）→ 401。
// 客户端据 401 判定"凭证被拒"（如跨区提示），非凭证问题不得复用 401。
func failByLoginError(c *gin.Context, err error) {
	var lockedErr *security.LoginLockedError
	switch {
	case errors.Is(err, security.ErrUserDisabled):
		response.Fail(c, http.StatusForbidden, 10001, err.Error())
	case errors.As(err, &lockedErr):
		response.Fail(c, http.StatusTooManyRequests, 10001, err.Error())
	case errors.Is(err, service.ErrAuthServiceUnavailable):
		response.Fail(c, http.StatusServiceUnavailable, 50001, err.Error())
	default:
		response.Fail(c, http.StatusUnauthorized, 10001, err.Error())
	}
}

func GenerateCaptcha(c *gin.Context) {
	id, b64s, err := service.GenerateCaptcha()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10005, "生成验证码失败")
		return
	}
	response.OK(c, map[string]interface{}{
		"captcha_id": id,
		"b64s":       b64s,
	})
}

type sendCodeReq struct {
	Email        string `json:"email" binding:"required,email"`
	Scene        string `json:"scene" binding:"required"` // register, reset, etc.
	CaptchaID    string `json:"captcha_id"`
	CaptchaValue string `json:"captcha_value"`
}

func SendEmailCode(c *gin.Context) {
	var req sendCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	lang := i18n.RequestLanguage(c)
	if err := service.SendEmailCode(c.ClientIP(), req.Email, req.Scene, req.CaptchaID, req.CaptchaValue, lang); err != nil {
		response.Fail(c, http.StatusBadRequest, 10005, err.Error())
		return
	}

	response.OK(c, nil)
}

type oauthReq struct {
	IDToken  string `json:"id_token" binding:"required"`
	DeviceID string `json:"device_id" binding:"required"`
	Platform string `json:"platform" binding:"required"`
}

func LoginWithGoogle(c *gin.Context) {
	var req oauthReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	lang := i18n.RequestLanguage(c)
	data, err := service.LoginWithGoogle(req.IDToken, req.DeviceID, req.Platform, lang)
	if err != nil {
		failByLoginError(c, err)
		return
	}
	response.OK(c, data)
}

func LoginWithApple(c *gin.Context) {
	var req oauthReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	lang := i18n.RequestLanguage(c)
	data, err := service.LoginWithApple(req.IDToken, req.DeviceID, req.Platform, lang)
	if err != nil {
		failByLoginError(c, err)
		return
	}
	response.OK(c, data)
}

type registerReq struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required"`
	EmailCode string `json:"email_code" binding:"required"`
	DeviceID  string `json:"device_id" binding:"required"`
	Platform  string `json:"platform" binding:"required"`
	Region    string `json:"region"` // 可选：cn 或 global，空则使用服务端默认区域
}

func Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	lang := i18n.RequestLanguage(c)
	data, err := service.Register(req.Email, req.Password, req.EmailCode, req.DeviceID, req.Platform, lang, req.Region)
	if err != nil {
		response.Fail(c, http.StatusUnauthorized, 10001, err.Error())
		return
	}
	response.OK(c, data)
}

type loginReq struct {
	Account  string `json:"account" binding:"required"`  // Can be username or email
	Password string `json:"password" binding:"required"` // Assuming renamed from PwdHash
	DeviceID string `json:"device_id" binding:"required"`
	Platform string `json:"platform" binding:"required"`
}

func Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	lang := i18n.RequestLanguage(c)
	data, err := service.Login(req.Account, req.Password, req.DeviceID, req.Platform, lang)
	if err != nil {
		failByLoginError(c, err)
		return
	}
	response.OK(c, data)
}

type resetPasswordReq struct {
	Email       string `json:"email" binding:"required,email"`
	NewPassword string `json:"new_password" binding:"required"`
	EmailCode   string `json:"email_code" binding:"required"`
}

func ResetPassword(c *gin.Context) {
	var req resetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	if err := service.ResetPassword(req.Email, req.NewPassword, req.EmailCode); err != nil {
		response.Fail(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	response.OK(c, nil)
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, err := service.RefreshToken(req.RefreshToken)
	if err != nil {
		// 服务端自己的故障不能报成 401——客户端把 401 当作"会话已失效"，会清掉本地
		// 登录态。存储层抖一下就让所有正在刷新令牌的在线用户掉线，代价远大于让他们
		// 稍后重试。登录路径早有这个区分，refresh 一直漏了。
		if errors.Is(err, service.ErrAuthServiceUnavailable) {
			response.Fail(c, http.StatusServiceUnavailable, 50001, err.Error())
			return
		}
		response.Fail(c, http.StatusUnauthorized, 10002, err.Error())
		return
	}
	response.OK(c, data)
}

type logoutReq struct {
	DeviceID string `json:"device_id"`
}

func Logout(c *gin.Context) {
	var req logoutReq
	_ = c.ShouldBindJSON(&req)
	userID := c.GetInt64("user_id")
	sessionID := ""
	accessJTI := ""
	var accessExp time.Time
	if claims := middleware.GetClaims(c); claims != nil {
		sessionID = claims.SessionID
		accessJTI = claims.ID
		if claims.ExpiresAt != nil {
			accessExp = claims.ExpiresAt.Time
		}
	}
	if err := service.LogoutWithToken(userID, req.DeviceID, sessionID, accessJTI, accessExp); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10005, "退出登录失败")
		return
	}
	response.OK(c, nil)
}

// IssueWatchTokens hands the caller a token pair for their Apple Watch, in a
// refresh family of its own. The watch cannot log in, so the phone asks on its
// behalf with its own access token; the phone's refresh token never leaves the
// phone.
func IssueWatchTokens(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, err := service.IssueWatchTokens(userID)
	if err != nil {
		failByLoginError(c, err)
		return
	}
	response.OK(c, data)
}
