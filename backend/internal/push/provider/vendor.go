package provider

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VendorSender 是国产厂商系统级推送通道的统一契约。
// 各厂商 provider 实现该接口后即可被 push worker 按设备平台路由。
type VendorSender interface {
	Name() string
	Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error)
	IsTokenInvalid(result *PushResult) bool
}

// pushIntentScheme 是客户端 AndroidManifest 中为厂商通道通知点击注册的自定义 scheme。
// 厂商通道的通知由系统代收代展示，点击行为必须由服务端在下发时以 intent 指定。
const pushIntentScheme = "grixpush"

// pushIntentPackage 为客户端应用包名，用于把点击意图限定到本应用。
const pushIntentPackage = "pub.dhf.grix"

// vendorExtras 构造随通知下发的自定义键值对。
// 键名与 FCM / JPush 通道保持一致，客户端 checkPushTapIntent 按同一组键读取。
//
// 只携带 session_id 与 recipient_id：厂商通道的通知由系统渲染，
// 头像与首字母（FCM 自绘通知才需要）用不上；且二者取值来自用户昵称等可控内容，
// 放进 intent scheme 会带来注入面与编码损坏（如签名 URL 里的 `=`）。
// 这两个字段的值域是会话 ID 与数字用户 ID，注入面为零。
func vendorExtras(payload *PushPayload) map[string]string {
	extras := map[string]string{}
	if payload == nil {
		return extras
	}
	if payload.SessionID != "" {
		extras["session_id"] = payload.SessionID
	}
	if payload.RecipientID > 0 {
		extras["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
	}
	return extras
}

// vendorIntentURI 生成厂商通道通知的点击意图（intent scheme URI）。
// extras 以 `S.<key>=<value>` 形式随 intent 携带，客户端从 intent extras 中取回，
// 与 FCM data 消息落到同一个处理入口。
func vendorIntentURI(extras map[string]string) string {
	var b strings.Builder
	b.WriteString("intent://")
	b.WriteString(pushIntentPackage)
	b.WriteString("/push?#Intent;scheme=")
	b.WriteString(pushIntentScheme)
	b.WriteString(";launchFlags=0x4000000;package=")
	b.WriteString(pushIntentPackage)
	b.WriteString(";")
	for _, key := range sortedKeys(extras) {
		// 纵深防御：extras 的值域（会话 ID / 数字用户 ID）本不含分隔符，
		// 仍剔除一遍，确保任何取值都无法截断或伪造意图字段。
		value := strings.NewReplacer(";", "", "=", "").Replace(extras[key])
		b.WriteString("S.")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(value)
		b.WriteString(";")
	}
	b.WriteString("end")
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// accessTokenCache 缓存厂商鉴权接口签发的短期 access token。
// 华为 / 荣耀 / OPPO / vivo 均要求先换取 token 再下发，token 有效期从 1 小时到 24 小时不等。
type accessTokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time

	// inflight 非 nil 表示已有一次换取在进行中；其余调用者等它关闭即可，
	// 不再各自发一遍鉴权请求（单飞）。
	inflight chan struct{}
	// lastErr / retryAfter 构成负缓存：鉴权故障时不再每条推送都打一次厂商接口，
	// 否则极易被限流封禁。
	lastErr    error
	retryAfter time.Time

	now func() time.Time
}

const (
	// tokenRefreshSkew 提前于服务端过期时间刷新，避免边界时刻用到刚失效的 token。
	tokenRefreshSkew = 5 * time.Minute
	// tokenFetchBackoff 换取失败后的冷却窗口，期间直接复用上次错误，不再打厂商接口。
	tokenFetchBackoff = 10 * time.Second
	// tokenFetchTimeout 是换取与调用者 ctx 解绑后的兜底上限，防止无限期挂起。
	// 各 provider 的 http.Client 自带 10s 超时，实际闸门在那里；此值只对
	// 未设置 Timeout 的 client 起作用，故取一个更宽松的值。
	tokenFetchTimeout = 15 * time.Second
)

func (c *accessTokenCache) clock() func() time.Time {
	if c.now != nil {
		return c.now
	}
	return time.Now
}

// get 返回缓存中仍然有效的 token；已过期或未取过时调用 fetch 换取新 token。
// fetch 返回 token 及其有效期。
//
// 三条约束：
//   - 不持锁发 HTTP，否则换取期间该厂商所有推送串行阻塞；
//   - fetch 与调用者的 ctx 解绑（context.WithoutCancel），否则触发换取的那条消息一旦
//     被取消，所有等待者一起失败；
//   - 失败进入冷却窗口，避免鉴权故障时把厂商鉴权接口打到限流。
func (c *accessTokenCache) get(ctx context.Context, fetch func(context.Context) (string, time.Duration, error)) (string, error) {
	now := c.clock()

	var token string
	var err error
	for {
		c.mu.Lock()
		if c.token != "" && now().Before(c.expiresAt) {
			cached := c.token
			c.mu.Unlock()
			return cached, nil
		}
		if c.lastErr != nil && now().Before(c.retryAfter) {
			backoffErr := c.lastErr
			c.mu.Unlock()
			return "", backoffErr
		}
		if wait := c.inflight; wait != nil {
			c.mu.Unlock()
			// 等待进行中的换取；调用者自己的 ctx 被取消时只放弃等待，不影响换取本身。
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		done := make(chan struct{})
		c.inflight = done
		c.mu.Unlock()

		// defer 释放 inflight：fetch 若 panic，等待者不会被永久挂住。
		func() {
			defer func() {
				c.mu.Lock()
				c.inflight = nil
				c.mu.Unlock()
				close(done)
			}()
			token, err = c.fetchOnce(ctx, fetch, now)
		}()

		return token, err
	}
}

// fetchOnce 执行一次换取并落缓存（成功）或落冷却窗口（失败）。
func (c *accessTokenCache) fetchOnce(
	ctx context.Context,
	fetch func(context.Context) (string, time.Duration, error),
	now func() time.Time,
) (string, error) {
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenFetchTimeout)
	defer cancel()

	token, ttl, err := fetch(fetchCtx)
	if err == nil && strings.TrimSpace(token) == "" {
		err = fmt.Errorf("vendor auth returned empty access token")
	}
	// 有效期非正（厂商回包异常或字段缺失被解析成 0）时不得缓存：
	// 存下来会立刻判定过期，使每条推送都换一次 token；且成功路径不进冷却窗口，
	// 退避也拦不住，正是要避免的限流风险。当作换取失败走退避。
	if err == nil && ttl <= 0 {
		err = fmt.Errorf("vendor auth returned non-positive token ttl: %s", ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.lastErr = err
		c.retryAfter = now().Add(tokenFetchBackoff)
		return "", err
	}

	if ttl > tokenRefreshSkew {
		ttl -= tokenRefreshSkew
	}
	c.token = token
	c.expiresAt = now().Add(ttl)
	c.lastErr = nil
	c.retryAfter = time.Time{}
	return token, nil
}

// invalidate 丢弃缓存的 token，用于下发被判定为鉴权失效后强制重取。
//
// 有意只清 token，不碰 lastErr / retryAfter：
// 退避是针对厂商鉴权接口的全局闸门，任何路径都不得绕开它。一旦在此清掉，
// 厂商持续判定鉴权失效时，每条推送都会立刻重取一次 token，形成打向厂商鉴权接口的风暴。
//
// 代价：换取刚失败、又恰好收到鉴权失效回包时，会多等一个冷却窗口。这是有意的。
func (c *accessTokenCache) invalidate() {
	c.mu.Lock()
	c.token = ""
	c.expiresAt = time.Time{}
	c.mu.Unlock()
}

func vendorHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
