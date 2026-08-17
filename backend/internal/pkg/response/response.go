package response

import (
	"net/http"

	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

type R struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// internalErrorMsg 是 5xx 对外的统一固定文案（i18n 已有词条：en "Internal server error"）。
// err.Error() 等内部细节（SQL、堆栈、内部地址）只进服务端日志，绝不返回客户端。
const internalErrorMsg = "服务端内部异常"

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, R{Code: 0, Msg: "success", Data: data})
}

func Fail(c *gin.Context, httpCode int, bizCode int, msg string) {
	lang := i18n.RequestLanguage(c)
	if httpCode >= http.StatusInternalServerError {
		logInternalError(c, httpCode, bizCode, msg)
		msg = internalErrorMsg
	}
	localizedMsg := i18n.LocalizeMessage(msg, lang)
	c.JSON(httpCode, R{Code: bizCode, Msg: localizedMsg})
}

func FailWithData(c *gin.Context, httpCode int, bizCode int, msg string, data interface{}) {
	lang := i18n.RequestLanguage(c)
	if httpCode >= http.StatusInternalServerError {
		logInternalError(c, httpCode, bizCode, msg)
		msg = internalErrorMsg
	}
	localizedMsg := i18n.LocalizeMessage(msg, lang)
	c.JSON(httpCode, R{Code: bizCode, Msg: localizedMsg, Data: data})
}

// logInternalError 把被遮蔽的 5xx 原始错误落到服务端日志，带请求上下文便于排查。
func logInternalError(c *gin.Context, httpCode, bizCode int, msg string) {
	if logger.L == nil || c == nil || c.Request == nil {
		return
	}
	logger.L.Errorf("internal error masked: method=%s path=%s http=%d biz_code=%d err=%s",
		c.Request.Method, c.Request.URL.Path, httpCode, bizCode, msg)
}
