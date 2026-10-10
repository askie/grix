package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type agentAPIGatewayWalletResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Balance  string `json:"balance"`
		Currency string `json:"currency"`
	} `json:"data"`
}

func setupAgentAPIGatewayWalletTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	// 钱包自动创建路径用 snowflake 生成 ID,测试环境手动初始化节点。
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	r := gin.New()
	api := r.Group("/agent-api")
	api.Use(middleware.AgentAPIAuth())
	api.GET("/gateway/wallet", AgentAPIGatewayWallet)

	return r, testDB, func() { testDB.Close() }
}

func seedAgentAPIGatewayWalletAuth(
	t *testing.T,
	db *testutil.TestDB,
	ownerID, agentID int64,
	apiKey string,
) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "agent_wallet_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "agent_wallet_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}
	if err := db.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "agent_wallet_actor_" + strconv.FormatInt(agentID, 10),
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
		APIKeyHint:   agentapi.APIKeyHint(apiKey),
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed actor agent error: %v", err)
	}
}

func seedGatewayWallet(t *testing.T, db *testutil.TestDB, ownerID int64, balance string) {
	t.Helper()
	amount, err := decimal.NewFromString(balance)
	if err != nil {
		t.Fatalf("parse balance error: %v", err)
	}
	walletRow := model.GatewayWallet{
		ID:      ownerID*1000 + 1,
		OwnerID: ownerID,
		Balance: amount,
	}
	if err := db.DB.Create(&walletRow).Error; err != nil {
		t.Fatalf("seed wallet error: %v", err)
	}
}

func doAgentAPIGatewayWalletGet(t *testing.T, r *gin.Engine, apiKey string) (*httptest.ResponseRecorder, agentAPIGatewayWalletResp) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/agent-api/gateway/wallet", nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	var body agentAPIGatewayWalletResp
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response error: %v", err)
		}
	}
	return recorder, body
}

func TestAgentAPIGatewayWalletReturnsOwnerBalance(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIGatewayWalletTest(t)
	defer cleanup()

	seedAgentAPIGatewayWalletAuth(t, testDB, 7001, 9001, "wallet-key-1")
	// 高精度小数必须按字符串原样返回,不走 JSON number。
	seedGatewayWallet(t, testDB, 7001, "12.345678901234")

	recorder, body := doAgentAPIGatewayWalletGet(t, r, "wallet-key-1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if body.Data.Balance != "12.345678901234" {
		t.Fatalf("balance=%q want 12.345678901234", body.Data.Balance)
	}
	if body.Data.Currency != "USD" {
		t.Fatalf("currency=%q want USD", body.Data.Currency)
	}
}

func TestAgentAPIGatewayWalletAutoCreatesZeroWallet(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIGatewayWalletTest(t)
	defer cleanup()

	seedAgentAPIGatewayWalletAuth(t, testDB, 7002, 9002, "wallet-key-2")

	recorder, body := doAgentAPIGatewayWalletGet(t, r, "wallet-key-2")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if body.Data.Balance != "0" {
		t.Fatalf("balance=%q want 0", body.Data.Balance)
	}
}

func TestAgentAPIGatewayWalletOwnerIsolation(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIGatewayWalletTest(t)
	defer cleanup()

	seedAgentAPIGatewayWalletAuth(t, testDB, 7003, 9003, "wallet-key-3")
	seedAgentAPIGatewayWalletAuth(t, testDB, 7004, 9004, "wallet-key-4")
	seedGatewayWallet(t, testDB, 7003, "100")
	seedGatewayWallet(t, testDB, 7004, "0.5")

	_, bodyA := doAgentAPIGatewayWalletGet(t, r, "wallet-key-3")
	if bodyA.Data.Balance != "100" {
		t.Fatalf("owner A balance=%q want 100", bodyA.Data.Balance)
	}
	_, bodyB := doAgentAPIGatewayWalletGet(t, r, "wallet-key-4")
	if bodyB.Data.Balance != "0.5" {
		t.Fatalf("owner B balance=%q want 0.5", bodyB.Data.Balance)
	}
}

func TestAgentAPIGatewayWalletRejectsBadCredential(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIGatewayWalletTest(t)
	defer cleanup()

	seedAgentAPIGatewayWalletAuth(t, testDB, 7005, 9005, "wallet-key-5")

	recorder, _ := doAgentAPIGatewayWalletGet(t, r, "")
	if recorder.Code == http.StatusOK {
		t.Fatalf("missing key unexpectedly succeeded: %s", recorder.Body.String())
	}
	recorder, _ = doAgentAPIGatewayWalletGet(t, r, "wrong-key")
	if recorder.Code == http.StatusOK {
		t.Fatalf("wrong key unexpectedly succeeded: %s", recorder.Body.String())
	}
}
