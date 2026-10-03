package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func apiGetSystemControls(c *gin.Context) {
	items, err := adminservice.ListSystemControls()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, "系统控制读取失败")
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Decode exactly one object with exactly one explicit value, including false.
// Reject duplicate/unknown fields and trailing JSON rather than silently accepting it.
func decodeSystemControlValue(r io.Reader) (json.RawMessage, error) {
	d := json.NewDecoder(r)
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, adminservice.ErrInvalidSystemControl
	}
	var value json.RawMessage
	for d.More() {
		token, err = d.Token()
		if err != nil || token != "value" || value != nil {
			return nil, adminservice.ErrInvalidSystemControl
		}
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
	}
	if _, err = d.Token(); err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, adminservice.ErrInvalidSystemControl
	}
	if value == nil {
		return nil, adminservice.ErrInvalidSystemControl
	}
	return value, nil
}

func apiUpdateSystemControl(c *gin.Context) {
	raw, err := decodeSystemControlValue(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "必须显式提交且仅提交 value")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	item, err := adminservice.UpdateSystemControl(admin.ID, c.Param("key"), raw, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if errors.Is(err, adminservice.ErrInvalidSystemControl) {
			response.Fail(c, http.StatusBadRequest, 10002, err.Error())
		} else {
			response.Fail(c, http.StatusInternalServerError, 10004, "系统控制保存失败")
		}
		return
	}
	response.OK(c, item)
}
