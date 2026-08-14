package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendorIntentURICarriesExtras(t *testing.T) {
	uri := vendorIntentURI(map[string]string{
		"session_id":   "sess-1",
		"recipient_id": "42",
	})

	assert.True(t, strings.HasPrefix(uri, "intent://pub.dhf.grix/push?#Intent;"))
	assert.Contains(t, uri, "scheme=grixpush")
	assert.Contains(t, uri, "package=pub.dhf.grix")
	assert.Contains(t, uri, "S.session_id=sess-1")
	assert.Contains(t, uri, "S.recipient_id=42")
	assert.True(t, strings.HasSuffix(uri, ";end"))
}

// intent scheme 以 `;` 和 `=` 作分隔符，值里出现这些字符会截断整个意图。
func TestVendorIntentURIStripsIntentDelimiters(t *testing.T) {
	uri := vendorIntentURI(map[string]string{"session_id": "a;b=c"})

	assert.Contains(t, uri, "S.session_id=abc;end")
}

func TestVendorExtrasOmitsEmptyFields(t *testing.T) {
	extras := vendorExtras(&PushPayload{SessionID: "sess-1"})

	assert.Equal(t, map[string]string{"session_id": "sess-1"}, extras)
}

// 厂商通道的通知由系统渲染，头像/首字母用不上；它们取值自用户昵称等可控内容，
// 放进 intent 会带来注入面，并让签名 URL 里的 `=` 被清洗坏。必须不下发。
func TestVendorExtrasExcludesUserControlledFields(t *testing.T) {
	extras := vendorExtras(&PushPayload{
		SessionID:       "sess-1",
		RecipientID:     42,
		SenderAvatarURL: "https://cos.example.com/a.png?q-sign-algorithm=sha1&q-ak=AKID",
		SenderInitial:   "郭",
	})

	assert.Equal(t, map[string]string{"session_id": "sess-1", "recipient_id": "42"}, extras)
}

func TestAccessTokenCacheReusesTokenUntilExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	cache := accessTokenCache{now: func() time.Time { return now }}

	fetches := 0
	fetch := func(context.Context) (string, time.Duration, error) {
		fetches++
		return "token-1", time.Hour, nil
	}

	for i := 0; i < 3; i++ {
		token, err := cache.get(context.Background(), fetch)
		require.NoError(t, err)
		assert.Equal(t, "token-1", token)
	}
	assert.Equal(t, 1, fetches, "token should be fetched once while valid")

	// 有效期 1 小时、提前量 5 分钟 → 55 分钟后必须重新换取。
	now = now.Add(56 * time.Minute)
	token, err := cache.get(context.Background(), fetch)
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)
	assert.Equal(t, 2, fetches, "expired token should trigger a refetch")
}

func TestAccessTokenCacheInvalidateForcesRefetch(t *testing.T) {
	cache := accessTokenCache{}
	fetches := 0
	fetch := func(context.Context) (string, time.Duration, error) {
		fetches++
		return "token", time.Hour, nil
	}

	_, err := cache.get(context.Background(), fetch)
	require.NoError(t, err)
	cache.invalidate()
	_, err = cache.get(context.Background(), fetch)
	require.NoError(t, err)

	assert.Equal(t, 2, fetches)
}

func TestAccessTokenCachePropagatesFetchError(t *testing.T) {
	cache := accessTokenCache{}
	_, err := cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		return "", 0, errors.New("auth down")
	})

	assert.ErrorContains(t, err, "auth down")
}

// 并发换取只打一次厂商鉴权接口（单飞），且不持锁发 HTTP：
// fetch 阻塞期间其它调用者应当在等待，而不是各自再发一次。
func TestAccessTokenCacheSingleFlightsConcurrentFetches(t *testing.T) {
	cache := accessTokenCache{}
	var fetches int32
	release := make(chan struct{})

	fetch := func(context.Context) (string, time.Duration, error) {
		atomic.AddInt32(&fetches, 1)
		<-release
		return "token", time.Hour, nil
	}

	const callers = 8
	errs := make(chan error, callers)
	tokens := make(chan string, callers)
	for i := 0; i < callers; i++ {
		go func() {
			token, err := cache.get(context.Background(), fetch)
			tokens <- token
			errs <- err
		}()
	}

	// 让所有 goroutine 都进到 get 里；此时只应有一次 fetch 在飞。
	require.Eventually(t, func() bool { return atomic.LoadInt32(&fetches) == 1 }, time.Second, 5*time.Millisecond)
	close(release)

	for i := 0; i < callers; i++ {
		require.NoError(t, <-errs)
		assert.Equal(t, "token", <-tokens)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetches), "concurrent callers must share one auth request")
}

// 触发换取的那条消息被取消，不能连累换取本身与其它等待者。
func TestAccessTokenCacheFetchSurvivesCallerCancellation(t *testing.T) {
	cache := accessTokenCache{}
	fetchStarted := make(chan struct{})
	fetchCtxDone := make(chan bool, 1)

	fetch := func(ctx context.Context) (string, time.Duration, error) {
		close(fetchStarted)
		// 调用者的 ctx 取消后，fetch 自己的 ctx 不应随之取消。
		select {
		case <-ctx.Done():
			fetchCtxDone <- true
		case <-time.After(200 * time.Millisecond):
			fetchCtxDone <- false
		}
		return "token", time.Hour, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-fetchStarted
		cancel()
	}()

	token, err := cache.get(ctx, fetch)

	// 发起者自己拿到的是 ctx 错误或正常 token（取决于取消时机），但换取必须没被取消。
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	} else {
		assert.Equal(t, "token", token)
	}
	assert.False(t, <-fetchCtxDone, "fetch ctx must not be cancelled by the caller's ctx")
	cancel()
}

// 换取失败后进入冷却窗口：期间直接复用上次错误，不再打厂商鉴权接口。
func TestAccessTokenCacheBacksOffAfterFailure(t *testing.T) {
	now := time.Unix(2000, 0)
	cache := accessTokenCache{now: func() time.Time { return now }}

	fetches := 0
	failing := func(context.Context) (string, time.Duration, error) {
		fetches++
		return "", 0, errors.New("auth down")
	}

	for i := 0; i < 5; i++ {
		_, err := cache.get(context.Background(), failing)
		assert.ErrorContains(t, err, "auth down")
	}
	assert.Equal(t, 1, fetches, "failures inside the backoff window must not re-hit the vendor auth API")

	// 冷却窗口过后放行一次重试。
	now = now.Add(tokenFetchBackoff + time.Second)
	_, err := cache.get(context.Background(), failing)
	assert.ErrorContains(t, err, "auth down")
	assert.Equal(t, 2, fetches)
}

// 冷却窗口内换取成功后，负缓存必须清掉，不能继续返回旧错误。
func TestAccessTokenCacheClearsErrorAfterSuccess(t *testing.T) {
	now := time.Unix(3000, 0)
	cache := accessTokenCache{now: func() time.Time { return now }}

	_, err := cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		return "", 0, errors.New("auth down")
	})
	require.Error(t, err)

	now = now.Add(tokenFetchBackoff + time.Second)
	token, err := cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		return "token", time.Hour, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "token", token)

	// 再取一次应命中缓存，且不再报错。
	token, err = cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		t.Fatal("must not refetch while the token is valid")
		return "", 0, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "token", token)
}

// 有效期非正的 token 不得缓存：否则每条推送都会重新换取（成功路径不进冷却窗口，
// 退避拦不住），把厂商鉴权接口打到限流。应当作换取失败并退避。
func TestAccessTokenCacheRejectsNonPositiveTTL(t *testing.T) {
	now := time.Unix(4000, 0)
	cache := accessTokenCache{now: func() time.Time { return now }}

	fetches := 0
	fetch := func(context.Context) (string, time.Duration, error) {
		fetches++
		return "token", 0, nil
	}

	_, err := cache.get(context.Background(), fetch)
	assert.ErrorContains(t, err, "non-positive token ttl")

	// 已进入冷却窗口：不再重复打鉴权接口。
	_, err = cache.get(context.Background(), fetch)
	assert.ErrorContains(t, err, "non-positive token ttl")
	assert.Equal(t, 1, fetches, "a bad ttl must back off, not hammer the vendor auth API")
}

// invalidate 只丢 token，不得清掉冷却窗口：
// 否则厂商持续判定鉴权失效时，每条推送都会立刻重取，形成鉴权接口风暴。
func TestAccessTokenCacheInvalidateKeepsBackoff(t *testing.T) {
	now := time.Unix(5000, 0)
	cache := accessTokenCache{now: func() time.Time { return now }}

	fetches := 0
	failing := func(context.Context) (string, time.Duration, error) {
		fetches++
		return "", 0, errors.New("auth down")
	}

	_, err := cache.get(context.Background(), failing)
	require.Error(t, err)
	assert.Equal(t, 1, fetches)

	cache.invalidate()

	_, err = cache.get(context.Background(), failing)
	assert.ErrorContains(t, err, "auth down")
	assert.Equal(t, 1, fetches, "invalidate must not bypass the failure backoff window")
}

// fetch panic 时必须释放 inflight，否则后续所有调用者被永久挂住。
func TestAccessTokenCacheReleasesInflightOnPanic(t *testing.T) {
	cache := accessTokenCache{}

	func() {
		defer func() { assert.NotNil(t, recover(), "panic must propagate to the caller") }()
		_, _ = cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
			panic("boom")
		})
	}()

	// inflight 已释放：下一个调用者能正常换取，而不是卡在等待。
	done := make(chan struct{})
	go func() {
		defer close(done)
		token, err := cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
			return "token", time.Hour, nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "token", token)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("caller blocked forever: inflight was not released after panic")
	}
}

func TestAccessTokenCacheRejectsEmptyToken(t *testing.T) {
	cache := accessTokenCache{}
	_, err := cache.get(context.Background(), func(context.Context) (string, time.Duration, error) {
		return "", time.Hour, nil
	})

	assert.ErrorContains(t, err, "empty access token")
}

// newHuaweiTestProvider 把华为 provider 的鉴权与下发端点都指向本地测试服务器。
func newHuaweiTestProvider(authURL, pushURL string) *HuaweiProvider {
	p := NewHuawei("app-1", "client-1", "secret-1")
	p.authURL = authURL
	p.pushURL = pushURL
	return p
}

func TestHuaweiSendSuccess(t *testing.T) {
	logger.Init()

	authCalls := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "grant_type=client_credentials")
		assert.Contains(t, string(body), "client_id=client-1")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"hms-token","expires_in":3600}`))
	}))
	defer authServer.Close()

	var gotAuthHeader, gotPath, gotBody string
	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":"80000000","msg":"Success"}`))
	}))
	defer pushServer.Close()

	p := newHuaweiTestProvider(authServer.URL, pushServer.URL)
	result, err := p.Send(context.Background(), "device-token", &PushPayload{
		Title:       "标题",
		Body:        "正文",
		SessionID:   "sess-1",
		RecipientID: 42,
	})
	require.NoError(t, err)

	assert.True(t, result.Success)
	assert.Equal(t, "Bearer hms-token", gotAuthHeader)
	assert.Equal(t, "/v1/app-1/messages:send", gotPath)
	assert.Contains(t, gotBody, `"token":["device-token"]`)
	assert.Contains(t, gotBody, `"title":"标题"`)
	// 点击意图必须携带会话与收件人，客户端据此打开正确会话并拦截错投。
	assert.Contains(t, gotBody, "S.session_id=sess-1")
	assert.Contains(t, gotBody, "S.recipient_id=42")

	// 第二次下发复用缓存的 access token。
	_, err = p.Send(context.Background(), "device-token", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)
	assert.Equal(t, 1, authCalls)
}

func TestHuaweiSendReportsInvalidToken(t *testing.T) {
	logger.Init()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"hms-token","expires_in":3600}`))
	}))
	defer authServer.Close()
	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"80300007","msg":"All token invalid"}`))
	}))
	defer pushServer.Close()

	p := newHuaweiTestProvider(authServer.URL, pushServer.URL)
	result, err := p.Send(context.Background(), "stale-token", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)

	assert.False(t, result.Success)
	assert.Equal(t, "80300007", result.Reason)
	assert.True(t, p.IsTokenInvalid(result), "80300007 must deactivate the device token")
}

// 其它业务失败码不得触发设备解绑，否则会误杀正常设备。
func TestHuaweiOtherFailureKeepsToken(t *testing.T) {
	logger.Init()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"hms-token","expires_in":3600}`))
	}))
	defer authServer.Close()
	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"80300010","msg":"rate limited"}`))
	}))
	defer pushServer.Close()

	p := newHuaweiTestProvider(authServer.URL, pushServer.URL)
	result, err := p.Send(context.Background(), "token", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)

	assert.False(t, result.Success)
	assert.False(t, p.IsTokenInvalid(result))
}

// 鉴权失效码必须清掉缓存，否则会拿着过期 token 一直失败。
func TestHuaweiAuthFailureInvalidatesCachedToken(t *testing.T) {
	logger.Init()

	authCalls := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		w.Write([]byte(`{"access_token":"hms-token","expires_in":3600}`))
	}))
	defer authServer.Close()
	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"80200001","msg":"token expired"}`))
	}))
	defer pushServer.Close()

	p := newHuaweiTestProvider(authServer.URL, pushServer.URL)
	_, err := p.Send(context.Background(), "token", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)
	_, err = p.Send(context.Background(), "token", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)

	assert.Equal(t, 2, authCalls, "auth failure must force a token refetch on the next send")
}

func TestHuaweiSendRejectsEmptyToken(t *testing.T) {
	p := NewHuawei("app", "client", "secret")
	_, err := p.Send(context.Background(), "  ", &PushPayload{Title: "t"})
	assert.ErrorContains(t, err, "device token is empty")
}

func TestXiaomiSendSuccess(t *testing.T) {
	logger.Init()

	var gotAuth string
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, r.ParseForm())
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":"ok","code":0,"description":"成功"}`))
	}))
	defer server.Close()

	p := NewXiaomi("app-secret", "pub.dhf.grix")
	p.baseURL = server.URL

	result, err := p.Send(context.Background(), "reg-id", &PushPayload{
		Title:       "标题",
		Body:        "正文",
		SessionID:   "sess-1",
		RecipientID: 42,
	})
	require.NoError(t, err)

	assert.True(t, result.Success)
	assert.Equal(t, "key=app-secret", gotAuth)
	assert.Equal(t, "reg-id", form.Get("registration_id"))
	assert.Equal(t, "标题", form.Get("title"))
	assert.Equal(t, "正文", form.Get("description"))
	assert.Equal(t, "pub.dhf.grix", form.Get("restricted_package_name"))
	assert.Equal(t, "2", form.Get("extra.notify_effect"))
	assert.Contains(t, form.Get("extra.intent_uri"), "S.session_id=sess-1")
	assert.Equal(t, "sess-1", form.Get("extra.session_id"))
	assert.Equal(t, "42", form.Get("extra.recipient_id"))
}

func TestXiaomiSendFailureReportsReason(t *testing.T) {
	logger.Init()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"error","code":21301,"reason":"invalid registration_id"}`))
	}))
	defer server.Close()

	p := NewXiaomi("secret", "pub.dhf.grix")
	p.baseURL = server.URL

	result, err := p.Send(context.Background(), "reg-id", &PushPayload{Title: "t", Body: "b"})
	require.NoError(t, err)

	assert.False(t, result.Success)
	assert.Equal(t, "invalid registration_id", result.Reason)
	// 小米下发接口不能可靠反映 regid 失效，绝不据此解绑设备。
	assert.False(t, p.IsTokenInvalid(result))
}

func TestXiaomiSendRejectsEmptyToken(t *testing.T) {
	p := NewXiaomi("secret", "pub.dhf.grix")
	_, err := p.Send(context.Background(), "", &PushPayload{Title: "t"})
	assert.ErrorContains(t, err, "device token is empty")
}

// 两个厂商 provider 都必须满足 worker 依赖的 VendorSender 契约。
func TestVendorProvidersSatisfyInterface(t *testing.T) {
	var _ VendorSender = NewHuawei("a", "b", "c")
	var _ VendorSender = NewXiaomi("a", "b")
}
