package service

import (
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupBindEmailTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	return testDB, func() { testDB.Close() }
}

// createBindEmailUser 建一个「手机号注册」的账号：email 列为空。
func createBindEmailUser(t *testing.T, testDB *testutil.TestDB, id int64, username, email string) {
	t.Helper()
	user := model.User{
		ID:       id,
		Username: username,
		Email:    email,
		Nickname: username,
		Status:   model.UserStatusActive,
	}
	db := testDB.DB
	if email == "" {
		db = db.Omit("Email")
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
}

func TestBindUserEmailSuccess(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1001, "phone_user", "")
	if err := storeEmailCode("new@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	if err := BindUserEmail(1001, " new@example.com ", "654321"); err != nil {
		t.Fatalf("绑定邮箱失败: %v", err)
	}

	var user model.User
	if err := testDB.DB.First(&user, int64(1001)).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Fatalf("邮箱未写入: got=%q", user.Email)
	}
	// 验证码用后即焚
	if VerifyEmailCode("new@example.com", bindEmailScene, "654321") {
		t.Fatal("验证码绑定后应已作废")
	}
}

func TestBindUserEmailRejectsWrongCode(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1002, "phone_user2", "")
	if err := storeEmailCode("new2@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	err := BindUserEmail(1002, "new2@example.com", "000000")
	if !errors.Is(err, ErrBindEmailCodeInvalid) {
		t.Fatalf("期望验证码错误, got=%v", err)
	}
}

func TestBindUserEmailRejectsAlreadyBound(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1003, "email_user", "old@example.com")
	if err := storeEmailCode("new3@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	err := BindUserEmail(1003, "new3@example.com", "654321")
	if !errors.Is(err, ErrBindEmailAlreadyBound) {
		t.Fatalf("期望已绑定错误, got=%v", err)
	}
}

func TestBindUserEmailRejectsTakenEmail(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1004, "owner_user", "taken@example.com")
	createBindEmailUser(t, testDB, 1005, "phone_user3", "")
	if err := storeEmailCode("taken@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	err := BindUserEmail(1005, "taken@example.com", "654321")
	if !errors.Is(err, ErrBindEmailTaken) {
		t.Fatalf("期望邮箱被占用错误, got=%v", err)
	}
}

func TestSendBindEmailCodeRejectsInvalidEmail(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1006, "phone_user4", "")

	err := SendBindEmailCode(1006, "127.0.0.1", "not-an-email", "zh-CN")
	if !errors.Is(err, ErrBindEmailInvalid) {
		t.Fatalf("期望邮箱格式错误, got=%v", err)
	}
}

func TestSendBindEmailCodeStoresCodeForBindScene(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1007, "phone_user5", "")

	originalDispatcher := sendEmailCodeDispatcher
	var gotScene, gotEmail string
	sendEmailCodeDispatcher = func(email, scene, lang string) error {
		gotEmail, gotScene = email, scene
		return storeEmailCode(email, scene, "112233")
	}
	defer func() { sendEmailCodeDispatcher = originalDispatcher }()

	if err := SendBindEmailCode(1007, "127.0.0.1", "fresh@example.com", "zh-CN"); err != nil {
		t.Fatalf("发送绑定验证码失败: %v", err)
	}
	if gotEmail != "fresh@example.com" || gotScene != bindEmailScene {
		t.Fatalf("发码参数不符: email=%q scene=%q", gotEmail, gotScene)
	}
	if !VerifyEmailCode("fresh@example.com", bindEmailScene, "112233") {
		t.Fatal("验证码未按 bind_email 场景存储")
	}
}

func TestBindUserEmailAllowsReplacingAppleRelayAddress(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1008, "apple_user", "abc123@privaterelay.appleid.com")
	if err := storeEmailCode("real@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	if err := BindUserEmail(1008, "real@example.com", "654321"); err != nil {
		t.Fatalf("Apple 中转邮箱应允许改绑: %v", err)
	}

	var user model.User
	if err := testDB.DB.First(&user, int64(1008)).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if user.Email != "real@example.com" {
		t.Fatalf("邮箱未替换: got=%q", user.Email)
	}
}

func TestBindUserEmailRejectsRelayTarget(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1009, "phone_user6", "")

	err := BindUserEmail(1009, "abc@privaterelay.appleid.com", "654321")
	if !errors.Is(err, ErrBindEmailRelayTarget) {
		t.Fatalf("期望拒绝 Apple 中转邮箱, got=%v", err)
	}
}

func TestBindUserEmailNormalizesToLowercase(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1010, "phone_user7", "")
	if err := storeEmailCode("mixed@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	// 用户输入带大写，发码与落库都按小写走，OAuth 才能按邮箱认领到同一账号。
	if err := BindUserEmail(1010, "Mixed@Example.com", "654321"); err != nil {
		t.Fatalf("绑定邮箱失败: %v", err)
	}

	var user model.User
	if err := testDB.DB.First(&user, int64(1010)).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if user.Email != "mixed@example.com" {
		t.Fatalf("邮箱未归一为小写: got=%q", user.Email)
	}
}

func TestBindUserEmailRejectsTakenEmailIgnoringCase(t *testing.T) {
	testDB, cleanup := setupBindEmailTest(t)
	defer cleanup()

	createBindEmailUser(t, testDB, 1011, "owner_user2", "Taken@Example.com")
	createBindEmailUser(t, testDB, 1012, "phone_user8", "")
	if err := storeEmailCode("taken@example.com", bindEmailScene, "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}

	err := BindUserEmail(1012, "taken@example.com", "654321")
	if !errors.Is(err, ErrBindEmailTaken) {
		t.Fatalf("大小写不同的同一邮箱应判为已占用, got=%v", err)
	}
}

func TestEmailNeedsBinding(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"abc123@privaterelay.appleid.com", true},
		{"ABC123@PrivateRelay.AppleID.com", true},
		{"user@example.com", false},
	}
	for _, c := range cases {
		if got := EmailNeedsBinding(c.email); got != c.want {
			t.Fatalf("EmailNeedsBinding(%q) = %v, want %v", c.email, got, c.want)
		}
	}
}
