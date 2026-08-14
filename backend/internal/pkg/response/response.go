package response

import (
	"net/http"

	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/gin-gonic/gin"
)

type R struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, R{Code: 0, Msg: "success", Data: data})
}

func Fail(c *gin.Context, httpCode int, bizCode int, msg string) {
	lang := i18n.RequestLanguage(c)
	localizedMsg := i18n.LocalizeMessage(msg, lang)
	c.JSON(httpCode, R{Code: bizCode, Msg: localizedMsg})
}

func FailWithData(c *gin.Context, httpCode int, bizCode int, msg string, data interface{}) {
	lang := i18n.RequestLanguage(c)
	localizedMsg := i18n.LocalizeMessage(msg, lang)
	c.JSON(httpCode, R{Code: bizCode, Msg: localizedMsg, Data: data})
}
