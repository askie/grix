package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LoginResp struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
	ExpiresIn    int64      `json:"expires_in"`
	User         model.User `json:"user"`
	WsEndpoint   string     `json:"ws_endpoint"` // 客户端应连接的 WS 地址
	Region       string     `json:"region"`      // 用户所在区域
}

const revokedAccessTokenKeyPrefix = "auth:revoked:access:"

func Register(email, password, emailCode, deviceID, platform, language, region string) (*LoginResp, error) {
	// 先去掉首尾空白：带空格的写法既会绕开判重，也会在库里留下取不回来的脏邮箱。
	email = strings.TrimSpace(email)
	registerEnabled, err := featuregate.IsPublicFeatureEnabled("auth_register")
	if err != nil {
		return nil, err
	}
	if !registerEnabled {
		return nil, errors.New("系统已关闭注册")
	}
	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return nil, err
	}
	if err := ValidateUserPassword(password); err != nil {
		return nil, err
	}
	if !VerifyEmailCode(email, "register", emailCode) {
		return nil, errors.New("邮箱验证码错误或已过期")
	}

	// 判重忽略大小写：uni_users_email 不折叠大小写，精确判重会把 Foo@x.com 和
	// foo@x.com 放行成两个账号，而认领与绑定链路都已把它们当同一个人。
	var existing model.User
	if err := store.DB.Where(
		"LOWER(email) = ?",
		strings.ToLower(email),
	).First(&existing).Error; err == nil {
		return nil, errors.New("注册失败，请检查邮箱验证码后重试")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("密码加密失败")
	}

	// 确定区域：优先使用客户端传入值，否则使用服务端配置的默认区域
	resolvedRegion := strings.TrimSpace(region)
	if resolvedRegion == "" {
		resolvedRegion = config.C.Server.Region
	}
	if resolvedRegion == "" {
		resolvedRegion = "global"
	}

	for i := 0; i < 5; i++ {
		username, nickname, err := buildRegisterIdentity(email)
		if err != nil {
			return nil, err
		}

		user := model.User{
			ID:           snowflake.GenID(),
			Email:        email,
			Username:     username,
			PasswordHash: string(hash),
			Nickname:     nickname,
			Region:       resolvedRegion,
		}

		var resp *LoginResp
		var staleDevices []model.Device
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
			if err := createDefaultUserSettingsTx(tx, user.ID, language); err != nil {
				return err
			}
			if err := addConfiguredCustomerFriendTx(tx, user.ID, settings.AutoAddCustomerUserID); err != nil {
				return err
			}

			issued, stale, err := issueTokenWithDB(tx, user, deviceID, platform)
			if err != nil {
				return err
			}
			resp = issued
			staleDevices = stale
			return nil
		}); err != nil {
			if isDuplicatedEmailErr(err) {
				return nil, errors.New("注册失败，请检查邮箱验证码后重试")
			}
			if isDuplicatedUsernameErr(err) {
				continue
			}
			return nil, normalizeRegisterTransactionErr(err)
		}

		cleanStaleDeviceCacheAfterCommit(user.ID, deviceID, staleDevices)
		notifyConfiguredCustomerFriendAdded(user.ID, settings.AutoAddCustomerUserID)
		scheduleRegisterWelcomeCompensation(user.ID, settings.AutoAddCustomerUserID)
		return resp, nil
	}

	return nil, errors.New("注册失败: 用户名生成冲突，请重试")
}

func normalizeRegisterTransactionErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrConfiguredCustomerInvalid) ||
		errors.Is(err, ErrConfiguredCustomerNotFound) ||
		errors.Is(err, ErrConfiguredCustomerDisabled) {
		return err
	}
	return errors.New("注册失败，请稍后重试")
}

// dummyUserPasswordHash 是一个固定的合法 bcrypt 哈希，用于在账号不存在时
// 执行一次等价耗时的比对，抹平登录响应的时序差异，防止账号枚举。
const dummyUserPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.eG1H0B/XQsI3a3hOyC"

// ErrAuthServiceUnavailable 登录依赖的基础设施（数据库/Redis）暂时不可用。
// handler 层据此返回 503，与"凭证错误"的 401 区分开。
var ErrAuthServiceUnavailable = errors.New("登录服务暂时不可用，请稍后再试")

func Login(account, password, deviceID, platform, language string) (*LoginResp, error) {
	ctx := context.Background()
	loginAccount := strings.TrimSpace(account)
	var user model.User
	// email 一侧忽略大小写：绑定链路把邮箱统一存小写，用户按自己记的写法登录也要能进。
	// username 仍按原样精确匹配。
	if err := store.DB.Where(
		"LOWER(email) = ? OR username = ?",
		strings.ToLower(loginAccount), loginAccount,
	).First(&user).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAuthServiceUnavailable
		}

		// 账号不存在时也执行一次等价的 bcrypt 比对，抹平时序差异，避免枚举已注册账号。
		_ = bcrypt.CompareHashAndPassword([]byte(dummyUserPasswordHash), []byte(password))

		accountGuard := security.NewUserLoginGuardByAccount(loginAccount)
		locked, remaining, guardErr := accountGuard.IsLocked(ctx)
		if guardErr != nil {
			return nil, ErrAuthServiceUnavailable
		}
		if locked {
			return nil, &security.LoginLockedError{Remaining: remaining}
		}

		_, lockedNow, lockDuration, guardErr := accountGuard.RecordFailure(ctx)
		if guardErr != nil {
			return nil, ErrAuthServiceUnavailable
		}
		if lockedNow {
			return nil, &security.LoginLockedError{Remaining: lockDuration}
		}
		return nil, errors.New("用户不存在或密码错误")
	}
	if err := security.EnsureUserActive(user.ID); err != nil {
		if errors.Is(err, security.ErrUserDisabled) {
			return nil, err
		}
		if errors.Is(err, security.ErrUserNotFound) {
			return nil, errors.New("用户不存在")
		}
		// DB 故障等基础设施错误不得伪装成凭证错误（会误触发客户端 401 语义）。
		return nil, ErrAuthServiceUnavailable
	}

	userGuard := security.NewUserLoginGuardByUserID(user.ID)
	locked, remaining, err := userGuard.IsLocked(ctx)
	if err != nil {
		return nil, ErrAuthServiceUnavailable
	}
	if locked {
		return nil, &security.LoginLockedError{Remaining: remaining}
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		_, lockedNow, lockDuration, guardErr := userGuard.RecordFailure(ctx)
		if guardErr != nil {
			return nil, ErrAuthServiceUnavailable
		}
		if lockedNow {
			return nil, &security.LoginLockedError{Remaining: lockDuration}
		}
		return nil, errors.New("用户不存在或密码错误")
	}

	if err := userGuard.RecordSuccess(ctx); err != nil {
		return nil, ErrAuthServiceUnavailable
	}

	return doIssueToken(user, deviceID, platform, language)
}

func ResetPassword(email, newPassword, emailCode string) error {
	email = strings.TrimSpace(email)
	if err := ValidateUserPassword(newPassword); err != nil {
		return err
	}
	if !VerifyEmailCode(email, "reset", emailCode) {
		return errors.New("邮箱验证码错误或已过期")
	}

	var user model.User
	if err := store.DB.Where(
		"LOWER(email) = ?",
		strings.ToLower(email),
	).First(&user).Error; err != nil {
		return errors.New("用户不存在")
	}
	if err := security.EnsureUserActive(user.ID); err != nil {
		if err == security.ErrUserDisabled {
			return err
		}
		return errors.New("用户不存在")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("密码加密失败")
	}

	if err := store.DB.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
		return err
	}

	changedAt := time.Now().UTC()
	if err := security.MarkUserPasswordChanged(user.ID, changedAt); err != nil {
		return err
	}
	if err := LogoutWithToken(user.ID, "", "", "", time.Time{}); err != nil {
		return err
	}
	return nil
}

func doIssueToken(user model.User, deviceID, platform, language string) (*LoginResp, error) {
	var resp *LoginResp
	var staleDevices []model.Device
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := updateUserPreferredLanguageTx(tx, user.ID, language); err != nil {
			return err
		}
		issued, stale, err := issueTokenWithDB(tx, user, deviceID, platform)
		if err != nil {
			return err
		}
		resp = issued
		staleDevices = stale
		return nil
	}); err != nil {
		return nil, err
	}
	cleanStaleDeviceCacheAfterCommit(user.ID, deviceID, staleDevices)
	userpref.InvalidatePreferredLanguage(user.ID)
	return resp, nil
}

// issueTokenWithDB 在登录/注册事务内签发令牌并失效该设备上其他账号的推送绑定。
// 返回的 staleDevices 是被失效的其他账号设备，其 Redis 缓存清理必须由调用方在事务
// 提交后进行——若在提交前清缓存，并发读者可能用尚未提交的旧值（is_active=true）回填，
// 造成缓存脏读回填。
func issueTokenWithDB(db *gorm.DB, user model.User, deviceID, platform string) (*LoginResp, []model.Device, error) {
	if db == nil {
		return nil, nil, errors.New("登录态初始化失败")
	}

	familyID := uuid.NewString()
	accessToken, expiresIn, err := jwtpkg.GenerateAccessTokenWithSession(user.ID, familyID)
	if err != nil {
		return nil, nil, err
	}
	refreshToken, refreshJTI, refreshExpiresAt, err := jwtpkg.GenerateRefreshTokenWithFamily(user.ID, familyID, "")
	if err != nil {
		return nil, nil, err
	}
	if err := db.Create(&model.RefreshToken{
		JTI:       refreshJTI,
		UserID:    user.ID,
		FamilyID:  familyID,
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: refreshExpiresAt,
	}).Error; err != nil {
		return nil, nil, errors.New("登录态初始化失败")
	}
	if err := RegisterLoginDeviceSessionOnGrantTx(db, user.ID, familyID, deviceID, platform); err != nil {
		return nil, nil, err
	}

	// 设备易主：本账号在该物理设备登录后，立即失效该设备上其他账号的推送绑定（DB 内完成），
	// 使后端给其他账号推送时不再命中这台设备（从源头止推，而非推后丢弃）。
	// 对应的 Redis 缓存清理交由调用方在事务提交后执行。
	staleDevices, err := DeactivateOtherUsersDeviceBindingsTx(db, user.ID, deviceID)
	if err != nil {
		return nil, nil, err
	}

	return &LoginResp{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		User:         user,
		WsEndpoint:   config.C.Server.PublicWsURL,
		Region:       user.Region,
	}, staleDevices, nil
}

// cleanStaleDeviceCacheAfterCommit 在登录/注册事务提交后清理被失效设备的 Redis 缓存。
// 必须在事务提交之后调用，避免提交前清缓存被脏读回填。
func cleanStaleDeviceCacheAfterCommit(userID int64, deviceID string, staleDevices []model.Device) {
	if len(staleDevices) == 0 {
		return
	}
	if err := removeUserDeviceCacheEntries(staleDevices); err != nil {
		logger.L.Warnf("clear stale device cache on login failed user=%d device=%s err=%v", userID, deviceID, err)
	}
}

func RefreshToken(refreshTokenStr string) (*LoginResp, error) {
	claims, err := jwtpkg.ValidateRefreshToken(refreshTokenStr)
	if err != nil {
		return nil, errors.New("refresh token 无效或已过期")
	}
	if strings.TrimSpace(claims.ID) == "" {
		return nil, errors.New("refresh token 无效或已过期")
	}

	now := time.Now().UTC()
	resp := &LoginResp{}

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var rt model.RefreshToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("jti = ? AND user_id = ?", claims.ID, claims.UserID).
			First(&rt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("refresh token 无效或已过期")
			}
			// 存储层故障。旧实现在这里把它伪装成"token 无效"，handler 再转成 401，
			// 客户端据此清掉本地会话——数据库抖一下就会把在线用户集体踢下线。
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		if rt.Status != model.RefreshTokenStatusActive {
			if err := revokeRefreshFamilyTx(tx, rt.UserID, rt.FamilyID, now); err != nil {
				return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
			}
			return errors.New("refresh token 已失效，请重新登录")
		}
		if now.After(rt.ExpiresAt) {
			if err := revokeRefreshFamilyTx(tx, rt.UserID, rt.FamilyID, now); err != nil {
				return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
			}
			return errors.New("refresh token 无效或已过期")
		}
		if claimFamily := strings.TrimSpace(claims.FamilyID); claimFamily != "" && claimFamily != rt.FamilyID {
			if err := revokeRefreshFamilyTx(tx, rt.UserID, rt.FamilyID, now); err != nil {
				return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
			}
			return errors.New("refresh token 已失效，请重新登录")
		}

		var user model.User
		if err := tx.First(&user, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("用户不存在")
			}
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		if err := security.EnsureUserActiveWithDB(tx, user.ID); err != nil {
			if errors.Is(err, security.ErrUserDisabled) {
				return err
			}
			if errors.Is(err, security.ErrUserNotFound) {
				return errors.New("用户不存在")
			}
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		accessToken, expiresIn, err := jwtpkg.GenerateAccessTokenWithSession(user.ID, rt.FamilyID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		newRefresh, newJTI, newRefreshExpiresAt, err := jwtpkg.GenerateRefreshTokenWithFamily(user.ID, rt.FamilyID, "")
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		updateRes := tx.Model(&model.RefreshToken{}).
			Where("jti = ? AND status = ?", rt.JTI, model.RefreshTokenStatusActive).
			Updates(map[string]interface{}{
				"status":          model.RefreshTokenStatusUsed,
				"used_at":         now,
				"updated_at":      now,
				"replaced_by_jti": newJTI,
			})
		if updateRes.Error != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, updateRes.Error)
		}
		if updateRes.RowsAffected == 0 {
			if err := revokeRefreshFamilyTx(tx, rt.UserID, rt.FamilyID, now); err != nil {
				return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
			}
			return errors.New("refresh token 已失效，请重新登录")
		}

		parentJTI := rt.JTI
		if err := tx.Create(&model.RefreshToken{
			JTI:       newJTI,
			UserID:    user.ID,
			FamilyID:  rt.FamilyID,
			Status:    model.RefreshTokenStatusActive,
			ParentJTI: &parentJTI,
			ExpiresAt: newRefreshExpiresAt,
		}).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		resp.AccessToken = accessToken
		resp.RefreshToken = newRefresh
		resp.ExpiresIn = expiresIn
		resp.User = user
		return nil
	}); err != nil {
		return nil, err
	}

	return resp, nil
}

func Logout(userID int64, deviceID string) error {
	return LogoutWithToken(userID, deviceID, "", "", time.Time{})
}

func LogoutWithToken(userID int64, deviceID, sessionID, accessJTI string, accessExpiresAt time.Time) error {
	now := time.Now().UTC()
	var revokedSessions []model.LoginDeviceSession

	if userID > 0 {
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			normalizedSessionID := strings.TrimSpace(sessionID)

			// A phone logout takes the watch with it. The watch has its own
			// refresh family precisely so the phone's rotation cannot touch it,
			// which also means a session-scoped revoke would leave it alive —
			// a signed-out phone with a still-working watch.
			familyIDs := []string(nil)
			if normalizedSessionID != "" {
				watchFamilies, err := listWatchFamilyIDsTx(tx, userID)
				if err != nil {
					return err
				}
				familyIDs = append([]string{normalizedSessionID}, watchFamilies...)
			}

			q := tx.Model(&model.RefreshToken{}).
				Where("user_id = ? AND status IN ?", userID, []int16{
					model.RefreshTokenStatusActive,
					model.RefreshTokenStatusUsed,
				})
			if len(familyIDs) > 0 {
				q = q.Where("family_id IN ?", familyIDs)
			}
			if err := q.Updates(map[string]interface{}{
				"status":     model.RefreshTokenStatusRevoked,
				"revoked_at": now,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}

			if normalizedSessionID != "" {
				for _, familyID := range familyIDs {
					session, err := setLoginDeviceSessionRevokedTx(tx, userID, familyID, now)
					if err != nil && !errors.Is(err, ErrLoginDeviceSessionNotFound) {
						return err
					}
					if session != nil {
						revokedSessions = append(revokedSessions, *session)
					}
				}
				return nil
			}

			sessions, err := setAllLoginDeviceSessionsRevokedTx(tx, userID, now)
			if err != nil {
				return err
			}
			revokedSessions = append(revokedSessions, sessions...)
			return nil
		}); err != nil {
			return err
		}
	}

	if store.RDB != nil {
		if jti := strings.TrimSpace(accessJTI); jti != "" && accessExpiresAt.After(now) {
			ttl := time.Until(accessExpiresAt)
			if ttl > 0 {
				if err := store.RDB.Set(context.Background(), revokedAccessTokenRedisKey(jti), "1", ttl).Err(); err != nil {
					return err
				}
			}
		}
	}

	for _, session := range revokedSessions {
		if err := security.MarkLoginSessionRevoked(userID, session.SessionID); err != nil {
			return err
		}
	}

	if deviceID != "" && store.RDB != nil {
		// Clean device from Redis routing
		if err := store.RDB.HDel(context.Background(), fmt.Sprintf("im:ws:route:%d", userID), deviceID).Err(); err != nil {
			return err
		}
		if err := store.RDB.Del(context.Background(), fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID)).Err(); err != nil {
			return err
		}
	}

	if err := deactivateUserDeviceBinding(userID, deviceID); err != nil {
		return err
	}
	return nil
}

func revokeRefreshFamilyTx(tx *gorm.DB, userID int64, familyID string, now time.Time) error {
	if strings.TrimSpace(familyID) == "" {
		return nil
	}
	return tx.Model(&model.RefreshToken{}).
		Where("user_id = ? AND family_id = ? AND status IN ?", userID, familyID, []int16{
			model.RefreshTokenStatusActive,
			model.RefreshTokenStatusUsed,
		}).
		Updates(map[string]interface{}{
			"status":     model.RefreshTokenStatusRevoked,
			"revoked_at": now,
			"updated_at": now,
		}).Error
}

func revokedAccessTokenRedisKey(jti string) string {
	return revokedAccessTokenKeyPrefix + jti
}
