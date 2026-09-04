package service

import (
	"testing"

	"gorm.io/datatypes"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// host_name 让手机端能按机器归类 agent（桌面端本来靠本机 connector 探测拿到）。
// 取自 connector 每次 WS 鉴权上报的 config.host_meta.hostname；没上报过的老
// connector 归"未知设备"，必须是空串而不是报错或漏字段。
func TestAgentListExposesHostName(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake error: %v", err)
	}

	const ownerID = int64(9751)
	agents := []model.Agent{
		{
			ID: 97511, OwnerID: ownerID, AgentName: "with-host",
			ProviderType: model.AgentProviderAPI, Status: model.AgentStatusActive,
			Config: datatypes.JSON([]byte(`{"host_meta":{"hostname":"gcf-mac","platform":"darwin"}}`)),
		},
		{
			ID: 97512, OwnerID: ownerID, AgentName: "no-host-meta",
			ProviderType: model.AgentProviderAPI, Status: model.AgentStatusActive,
			Config: datatypes.JSON([]byte(`{}`)),
		},
		{
			ID: 97513, OwnerID: ownerID, AgentName: "broken-config",
			ProviderType: model.AgentProviderAPI, Status: model.AgentStatusActive,
			Config: datatypes.JSON([]byte(`not-json`)),
		},
	}
	for i := range agents {
		if err := store.DB.Create(&agents[i]).Error; err != nil {
			t.Fatalf("create agent %d error: %v", agents[i].ID, err)
		}
	}

	list, err := AgentList(ownerID, nil)
	if err != nil {
		t.Fatalf("AgentList error: %v", err)
	}
	byID := make(map[int64]AgentResp, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	if got := byID[97511].HostName; got != "gcf-mac" {
		t.Fatalf("host_name=%q want=gcf-mac", got)
	}
	if got := byID[97512].HostName; got != "" {
		t.Fatalf("host_name=%q want empty for missing host_meta", got)
	}
	if got := byID[97513].HostName; got != "" {
		t.Fatalf("host_name=%q want empty for unparsable config", got)
	}
}
