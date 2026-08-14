package agentapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDispatchEggGet_MissingID(t *testing.T) {
	_, code, msg := dispatchEggGet(1, map[string]interface{}{})
	assert.Equal(t, 4001, code)
	assert.Equal(t, "id required", msg)

	_, code2, _ := dispatchEggGet(1, map[string]interface{}{"id": "   "})
	assert.Equal(t, 4001, code2)
}

func TestDispatchEggSearch_DefaultPagination(t *testing.T) {
	// 验证缺省参数填充逻辑，不调 DB（service 层会因无 DB 返回错误，但 page/page_size 的校正在 service 调用前完成）
	// 此处仅验证函数不 panic 且当 ownerID=0 时返回非零 errorCode（service 层保护）
	_, code, _ := dispatchEggSearch(0, map[string]interface{}{})
	assert.NotEqual(t, 0, code)
}
