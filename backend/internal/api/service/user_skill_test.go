package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupUserSkillTest(t *testing.T) func() {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	// 确保 user_skills 表存在（NewTestDB 若未含新模型则补建）。
	if err := store.DB.AutoMigrate(&model.UserSkill{}); err != nil {
		t.Fatalf("automigrate user_skills: %v", err)
	}
	return func() { testDB.Close() }
}

func TestCreateUserSkillAndDuplicate(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1001)

	s, ec := CreateUserSkill(owner, "  报告规范  ", "# 规范\n结论先行")
	if ec != nil {
		t.Fatalf("create failed: %v", ec)
	}
	if s.Name != "报告规范" {
		t.Fatalf("name not trimmed: %q", s.Name)
	}
	if s.Version != 1 {
		t.Fatalf("initial version want 1 got %d", s.Version)
	}
	if s.Digest != skillDigest("# 规范\n结论先行") {
		t.Fatalf("digest mismatch")
	}

	// 同名再建应报冲突。
	if _, ec := CreateUserSkill(owner, "报告规范", "别的内容"); ec == nil {
		t.Fatalf("expected duplicate name rejected")
	}
}

func TestUserSkillValidation(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1002)

	if _, ec := CreateUserSkill(owner, "   ", "x"); ec == nil {
		t.Fatalf("expected empty name rejected")
	}
	if _, ec := CreateUserSkill(owner, "n", "   "); ec == nil {
		t.Fatalf("expected empty content rejected")
	}
	if _, ec := CreateUserSkill(owner, strings.Repeat("字", 101), "x"); ec == nil {
		t.Fatalf("expected too-long name rejected")
	}
	if _, ec := CreateUserSkill(owner, "big", strings.Repeat("a", skillContentMaxLen+1)); ec == nil {
		t.Fatalf("expected too-large content rejected")
	}
}

func TestSkillNamePathSafety(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1101)
	for _, bad := range []string{"../evil", "a/b", "a\\b", "..", ".hidden", "foo..bar", "x\x00y"} {
		if _, ec := CreateUserSkill(owner, bad, "c"); ec == nil {
			t.Fatalf("unsafe name %q should be rejected", bad)
		}
	}
	// 正常名（含中文）应通过。
	if _, ec := CreateUserSkill(owner, "调研报告规范", "c"); ec != nil {
		t.Fatalf("normal name rejected: %v", ec)
	}
}

func TestSkillNameReservedPrefix(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1103)
	// grix- 前缀（不区分大小写）一律拒绝，防止内置技能被误传。
	for _, name := range []string{"grix-admin", "Grix-Query", "GRIX-ACCESS-CONTROL", "grix-"} {
		if _, ec := CreateUserSkill(owner, name, "c"); ec == nil {
			t.Fatalf("reserved prefix name %q should be rejected", name)
		} else if ec.BizCode != errcode.ErrSkillNameReserved.BizCode {
			t.Fatalf("reserved prefix name %q should return ErrSkillNameReserved, got %v", name, ec)
		}
		if _, ec := UpsertUserSkillByName(owner, name, "c"); ec == nil {
			t.Fatalf("reserved prefix name %q should be rejected on upsert", name)
		} else if ec.BizCode != errcode.ErrSkillNameReserved.BizCode {
			t.Fatalf("reserved prefix name %q should return ErrSkillNameReserved on upsert, got %v", name, ec)
		}
	}
	// 非 grix- 前缀的正常名不受影响。
	if _, ec := CreateUserSkill(owner, "grix", "c"); ec != nil {
		t.Fatalf("name 'grix' (no hyphen) should be allowed: %v", ec)
	}
	if _, ec := CreateUserSkill(owner, "my-grix-skill", "c"); ec != nil {
		t.Fatalf("name with grix infix should be allowed: %v", ec)
	}
}

func TestUpdateUserSkillRenameToReservedPrefix(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1104)
	s, ec := CreateUserSkill(owner, "normal-name", "content")
	if ec != nil {
		t.Fatalf("create failed: %v", ec)
	}
	// 改名为 grix- 前缀应被拒绝，且返回 ErrSkillNameReserved。
	if _, ec := UpdateUserSkill(owner, s.ID, "grix-hijack", "content"); ec == nil {
		t.Fatalf("rename to reserved prefix should be rejected")
	} else if ec.BizCode != errcode.ErrSkillNameReserved.BizCode {
		t.Fatalf("rename to reserved prefix should return ErrSkillNameReserved, got %v", ec)
	}
	// 原名保持不变。
	got, ec := GetUserSkillContent(owner, s.ID)
	if ec != nil {
		t.Fatalf("get failed: %v", ec)
	}
	if got.Name != "normal-name" {
		t.Fatalf("name should stay unchanged after rejected rename, got %q", got.Name)
	}
}

func TestUpsertIdempotentNoVersionBump(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1102)
	s1, _ := UpsertUserSkillByName(owner, "idem", "same content")
	// 相同内容再 upsert：版本不变、不重写。
	s2, ec := UpsertUserSkillByName(owner, "idem", "same content")
	if ec != nil {
		t.Fatalf("re-upsert failed: %v", ec)
	}
	if s2.Version != s1.Version {
		t.Fatalf("version bumped on unchanged content: %d -> %d", s1.Version, s2.Version)
	}
	// 内容变了才升版本。
	s3, _ := UpsertUserSkillByName(owner, "idem", "changed content")
	if s3.Version != s1.Version+1 {
		t.Fatalf("version should bump on change: got %d", s3.Version)
	}
}

func TestUpdateUserSkillVersionAndDigest(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1003)

	s, _ := CreateUserSkill(owner, "s1", "v1 content")
	updated, ec := UpdateUserSkill(owner, s.ID, "s1", "v2 content changed")
	if ec != nil {
		t.Fatalf("update failed: %v", ec)
	}
	if updated.Version != 2 {
		t.Fatalf("version want 2 got %d", updated.Version)
	}
	if updated.Digest != skillDigest("v2 content changed") {
		t.Fatalf("digest not updated")
	}

	// 读回确认落库。
	got, ec := GetUserSkillContent(owner, s.ID)
	if ec != nil {
		t.Fatalf("get content failed: %v", ec)
	}
	if got.Content != "v2 content changed" || got.Version != 2 {
		t.Fatalf("persisted state wrong: %+v", got)
	}
}

func TestUpdateUserSkillRenameCollision(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1004)

	CreateUserSkill(owner, "a", "ca")
	sb, _ := CreateUserSkill(owner, "b", "cb")
	// 把 b 改名成 a 应冲突。
	if _, ec := UpdateUserSkill(owner, sb.ID, "a", "cb2"); ec == nil {
		t.Fatalf("expected rename collision rejected")
	}
}

func TestUserSkillOwnerIsolation(t *testing.T) {
	defer setupUserSkillTest(t)()
	const ownerA, ownerB = int64(1005), int64(1006)

	sa, _ := CreateUserSkill(ownerA, "shared-name", "A content")
	// B 建同名允许（不同 owner）。
	if _, ec := CreateUserSkill(ownerB, "shared-name", "B content"); ec != nil {
		t.Fatalf("different owner same name should be allowed: %v", ec)
	}
	// B 读不到 A 的技能。
	if _, ec := GetUserSkillContent(ownerB, sa.ID); ec == nil {
		t.Fatalf("owner B should not read owner A skill by id")
	}
	// B 改/删不了 A 的技能。
	if _, ec := UpdateUserSkill(ownerB, sa.ID, "shared-name", "hack"); ec == nil {
		t.Fatalf("owner B should not update owner A skill")
	}
	if ec := DeleteUserSkill(ownerB, sa.ID); ec == nil {
		t.Fatalf("owner B should not delete owner A skill")
	}
	// A 列表只含自己 + 系统内置，不含 B 的。
	list, _ := ListUserSkills(ownerA)
	for _, it := range list {
		if it.OwnerID == ownerB {
			t.Fatalf("owner A list leaked owner B skill")
		}
	}
}

func TestUpsertUserSkillByName(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1007)

	// 首次 upsert = 新建。
	s1, ec := UpsertUserSkillByName(owner, "up", "first")
	if ec != nil || s1.Version != 1 {
		t.Fatalf("first upsert wrong: %v %+v", ec, s1)
	}
	// 再次 upsert 同名 = 更新、版本自增。
	s2, ec := UpsertUserSkillByName(owner, "up", "second")
	if ec != nil {
		t.Fatalf("second upsert failed: %v", ec)
	}
	if s2.ID != s1.ID || s2.Version != 2 || s2.Content != "second" {
		t.Fatalf("upsert did not update in place: %+v", s2)
	}
}

func TestDeleteUserSkillByNameIdempotent(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1008)

	CreateUserSkill(owner, "d", "c")
	if ec := DeleteUserSkillByName(owner, "d"); ec != nil {
		t.Fatalf("delete failed: %v", ec)
	}
	// 再删不存在的名字应幂等成功。
	if ec := DeleteUserSkillByName(owner, "d"); ec != nil {
		t.Fatalf("idempotent delete should succeed: %v", ec)
	}
	if _, ec := GetUserSkillByName(owner, "d"); ec == nil {
		t.Fatalf("skill should be gone")
	}
}

func TestListUserSkillsShadowsSystemSameName(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1201)

	// 种一条系统内置 + owner 建同名覆盖。
	sys := model.UserSkill{ID: 999101, OwnerID: 0, Name: "覆盖名", Content: "system", Version: 3, Digest: skillDigest("system")}
	if err := store.DB.Create(&sys).Error; err != nil {
		t.Fatalf("seed system skill: %v", err)
	}
	own, ec := CreateUserSkill(owner, "覆盖名", "owner content")
	if ec != nil {
		t.Fatalf("create owner skill: %v", ec)
	}

	list, ec := ListUserSkills(owner)
	if ec != nil {
		t.Fatalf("list failed: %v", ec)
	}
	// 同名只保留一条，且是 owner 自己的（遮蔽系统内置）。
	found := 0
	for _, it := range list {
		if it.Name == "覆盖名" {
			found++
			if it.ID != own.ID || it.OwnerID != owner {
				t.Fatalf("shadow should prefer owner skill, got %+v", it)
			}
		}
	}
	if found != 1 {
		t.Fatalf("same-name entries want 1 got %d", found)
	}
}

func TestSkillDuplicateKeyErrMapped(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1202)

	if _, ec := CreateUserSkill(owner, "撞名", "c1"); ec != nil {
		t.Fatalf("create: %v", ec)
	}
	// 绕过 check-then-insert 的前置查重，直接落库制造唯一索引冲突，
	// 模拟并发竞态下 Create 报错的形态，验证 isSkillDuplicateKeyErr 能识别。
	dup := model.UserSkill{ID: 999201, OwnerID: owner, Name: "撞名", Content: "c2", Version: 1, Digest: skillDigest("c2")}
	err := store.DB.Create(&dup).Error
	if err == nil {
		t.Fatalf("expected unique violation")
	}
	if !isSkillDuplicateKeyErr(err) {
		t.Fatalf("unique violation not recognized: %v", err)
	}
}

// 技能库落库成功后应向 chan:broadcast 广播变更（skill_sync 下发的跨进程一跳）。
func TestSkillMutationPublishesBroadcast(t *testing.T) {
	defer setupUserSkillTest(t)()
	store.RDB = testutil.NewMockRedis()
	const owner = int64(1301)

	ctx := context.Background()
	sub := store.RDB.Subscribe(ctx, "chan:broadcast")
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe broadcast: %v", err)
	}
	ch := sub.Channel()

	expectBroadcast := func(step string, wantName string) {
		t.Helper()
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string                              `json:"cmd"`
				Payload protocol.SkillLibraryChangedPayload `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("%s: decode broadcast: %v", step, err)
			}
			if envelope.Cmd != protocol.RedisCmdSkillLibraryChanged {
				t.Fatalf("%s: cmd want %s got %s", step, protocol.RedisCmdSkillLibraryChanged, envelope.Cmd)
			}
			if envelope.Payload.OwnerID != owner || envelope.Payload.Name != wantName {
				t.Fatalf("%s: payload wrong: %+v", step, envelope.Payload)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: no broadcast received", step)
		}
	}
	expectNoBroadcast := func(step string) {
		t.Helper()
		select {
		case msg := <-ch:
			t.Fatalf("%s: unexpected broadcast: %s", step, msg.Payload)
		case <-time.After(200 * time.Millisecond):
		}
	}

	s, ec := CreateUserSkill(owner, "同步通知", "c1")
	if ec != nil {
		t.Fatalf("create: %v", ec)
	}
	expectBroadcast("create", "同步通知")

	if _, ec := UpdateUserSkill(owner, s.ID, "同步通知", "c2"); ec != nil {
		t.Fatalf("update: %v", ec)
	}
	expectBroadcast("update", "同步通知")

	// 幂等 upsert（内容没变）不应广播。
	if _, ec := UpsertUserSkillByName(owner, "同步通知", "c2"); ec != nil {
		t.Fatalf("idempotent upsert: %v", ec)
	}
	expectNoBroadcast("idempotent upsert")

	if ec := DeleteUserSkillByName(owner, "同步通知"); ec != nil {
		t.Fatalf("delete: %v", ec)
	}
	expectBroadcast("delete", "同步通知")

	// 幂等重复删（没删到东西）不应广播。
	if ec := DeleteUserSkillByName(owner, "同步通知"); ec != nil {
		t.Fatalf("re-delete: %v", ec)
	}
	expectNoBroadcast("idempotent delete")
}

func TestSystemSkillReadonly(t *testing.T) {
	defer setupUserSkillTest(t)()
	const owner = int64(1009)

	// 直接种一条系统内置技能（owner_id=0）。
	sys := model.UserSkill{ID: 999001, OwnerID: 0, Name: "sys", Content: "system", Version: 1, Digest: skillDigest("system")}
	if err := store.DB.Create(&sys).Error; err != nil {
		t.Fatalf("seed system skill: %v", err)
	}
	// owner 可读（列表 + 按名）。
	if _, ec := GetUserSkillByName(owner, "sys"); ec != nil {
		t.Fatalf("owner should read system skill: %v", ec)
	}
	// owner 改不了、删不了系统内置。
	if _, ec := UpdateUserSkill(owner, sys.ID, "sys", "hack"); ec == nil {
		t.Fatalf("system skill should be readonly on update")
	}
	if ec := DeleteUserSkill(owner, sys.ID); ec == nil {
		t.Fatalf("system skill should be readonly on delete")
	}
}
