package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestVerificationEmailContent(t *testing.T) {
	tests := []struct {
		name           string
		lang           string
		wantSubject    string
		wantBodySubstr string
	}{
		{
			name:           "english",
			lang:           "en",
			wantSubject:    "Your verification code",
			wantBodySubstr: "your verification code is",
		},
		{
			name:           "english locale tag",
			lang:           "en-US",
			wantSubject:    "Your verification code",
			wantBodySubstr: "your verification code is",
		},
		{
			name:           "chinese",
			lang:           "zh-CN",
			wantSubject:    "您的验证码",
			wantBodySubstr: "您的验证码是",
		},
		{
			name:           "unknown fallback to chinese",
			lang:           "fr-FR",
			wantSubject:    "您的验证码",
			wantBodySubstr: "您的验证码是",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := verificationEmailContent("123456", tt.lang)
			if subject != tt.wantSubject {
				t.Fatalf("subject mismatch: got=%q want=%q", subject, tt.wantSubject)
			}
			if !strings.Contains(body, tt.wantBodySubstr) {
				t.Fatalf("body mismatch: got=%q, expected substring=%q", body, tt.wantBodySubstr)
			}
		})
	}
}

func TestGenerateEmailCodeFormat(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := generateEmailCode()
		if err != nil {
			t.Fatalf("generateEmailCode() error = %v", err)
		}
		if matched, _ := regexp.MatchString(`^\d{6}$`, code); !matched {
			t.Fatalf("expected 6-digit code, got %q", code)
		}
	}
}

func TestVerifyEmailCodeNoUniversalBypass(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	const (
		email = "verify@example.com"
		scene = "register"
	)

	key := "auth:email_code:" + scene + ":" + email
	if err := store.RDB.Set(context.Background(), key, "654321", 5*time.Minute).Err(); err != nil {
		t.Fatalf("seed email code failed: %v", err)
	}

	if VerifyEmailCode(email, scene, "123456") {
		t.Fatal("expected universal test code to be rejected")
	}
	if !VerifyEmailCode(email, scene, "654321") {
		t.Fatal("expected stored code to be accepted")
	}
}

func setupEmailServiceSendCodeTest(t *testing.T) func() {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	logger.Init()
	systemsetting.InvalidateAuthSettingsCache()
	featuregate.InvalidateCache()
	seedAuthFeatureGates(t)

	originalDispatcher := sendEmailCodeDispatcher
	sendEmailCodeDispatcher = sendEmailCodeInternal

	return func() {
		sendEmailCodeDispatcher = originalDispatcher
		systemsetting.InvalidateAuthSettingsCache()
		featuregate.InvalidateCache()
		testDB.Close()
	}
}

func TestSendEmailCodeRegisterDoesNotRequireCaptcha(t *testing.T) {
	cleanup := setupEmailServiceSendCodeTest(t)
	defer cleanup()

	sendCalls := 0
	sendEmailCodeDispatcher = func(email, scene, lang string) error {
		sendCalls++
		return nil
	}

	if err := SendEmailCode("127.0.0.1", "register@example.com", "register", "", "", "zh-CN"); err != nil {
		t.Fatalf("SendEmailCode() error = %v", err)
	}
	if sendCalls != 1 {
		t.Fatalf("expected send dispatcher to be called once, got %d", sendCalls)
	}
}

func TestSendEmailCodeAppliesCooldownByIPAndEmail(t *testing.T) {
	tests := []struct {
		name       string
		firstIP    string
		firstMail  string
		secondIP   string
		secondMail string
	}{
		{
			name:       "same ip blocked within cooldown",
			firstIP:    "127.0.0.1",
			firstMail:  "first@example.com",
			secondIP:   "127.0.0.1",
			secondMail: "second@example.com",
		},
		{
			name:       "same email blocked within cooldown",
			firstIP:    "127.0.0.1",
			firstMail:  "same@example.com",
			secondIP:   "127.0.0.2",
			secondMail: "same@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupEmailServiceSendCodeTest(t)
			defer cleanup()

			sendCalls := 0
			sendEmailCodeDispatcher = func(email, scene, lang string) error {
				sendCalls++
				return nil
			}

			if err := SendEmailCode(tt.firstIP, tt.firstMail, "register", "", "", "zh-CN"); err != nil {
				t.Fatalf("first SendEmailCode() error = %v", err)
			}

			err := SendEmailCode(tt.secondIP, tt.secondMail, "register", "", "", "zh-CN")
			if !errors.Is(err, ErrEmailCodeSendTooFrequent) {
				t.Fatalf("expected ErrEmailCodeSendTooFrequent, got %v", err)
			}
			if sendCalls != 1 {
				t.Fatalf("expected send dispatcher to be called once, got %d", sendCalls)
			}
		})
	}
}

func TestSendEmailCodeResetStillRequiresCaptcha(t *testing.T) {
	cleanup := setupEmailServiceSendCodeTest(t)
	defer cleanup()

	sendCalls := 0
	sendEmailCodeDispatcher = func(email, scene, lang string) error {
		sendCalls++
		return nil
	}

	err := SendEmailCode("127.0.0.1", "reset@example.com", "reset", "", "", "zh-CN")
	if err == nil || err.Error() != "图形验证码错误或已过期" {
		t.Fatalf("expected captcha error, got %v", err)
	}

	if err := currentCaptchaStore().Set("captcha-reset", "2468"); err != nil {
		t.Fatalf("seed captcha error: %v", err)
	}
	if err := SendEmailCode("127.0.0.1", "reset@example.com", "reset", "captcha-reset", "2468", "zh-CN"); err != nil {
		t.Fatalf("SendEmailCode() with captcha error = %v", err)
	}
	if sendCalls != 1 {
		t.Fatalf("expected send dispatcher to be called once, got %d", sendCalls)
	}
}

func TestSendEmailCodeRollbackOnFailure(t *testing.T) {
	cleanup := setupEmailServiceSendCodeTest(t)
	defer cleanup()

	sendCalls := 0
	sendEmailCodeDispatcher = func(email, scene, lang string) error {
		sendCalls++
		if sendCalls == 1 {
			return errors.New("邮件发送失败")
		}
		return nil
	}

	err := SendEmailCode("127.0.0.1", "retry@example.com", "register", "", "", "zh-CN")
	if err == nil || err.Error() != "邮件发送失败" {
		t.Fatalf("expected send failure, got %v", err)
	}

	if err := SendEmailCode("127.0.0.1", "retry@example.com", "register", "", "", "zh-CN"); err != nil {
		t.Fatalf("expected retry to succeed after rollback, got %v", err)
	}
	if sendCalls != 2 {
		t.Fatalf("expected send dispatcher to be called twice, got %d", sendCalls)
	}
}

func TestVerifyEmailCodeBruteForceProtection(t *testing.T) {
	store.RDB = testutil.NewMockRedis()

	email := "brute@example.com"
	scene := "reset"
	code := "123456"

	// 存入验证码
	key := emailVerifyCodePrefix + scene + ":" + email
	store.RDB.Set(context.Background(), key, code, emailVerifyCodeTTL)

	// 连续失败 emailVerifyMaxFailures-1 次，验证码仍存在
	for i := 0; i < emailVerifyMaxFailures-1; i++ {
		if VerifyEmailCode(email, scene, "000000") {
			t.Fatalf("wrong code should not pass (attempt %d)", i+1)
		}
		if v, _ := store.RDB.Get(context.Background(), key).Result(); v == "" {
			t.Fatalf("code should still exist after %d failures", i+1)
		}
	}

	// 第 emailVerifyMaxFailures 次失败：验证码应被作废
	if VerifyEmailCode(email, scene, "000000") {
		t.Fatal("wrong code should not pass on final attempt")
	}
	if v, _ := store.RDB.Get(context.Background(), key).Result(); v != "" {
		t.Fatal("code should be invalidated after max failures")
	}

	// 作废后正确码也无法通过
	if VerifyEmailCode(email, scene, code) {
		t.Fatal("correct code should not pass after invalidation")
	}
}

func TestVerifyEmailCodeSuccessClearsAttemptCounter(t *testing.T) {
	store.RDB = testutil.NewMockRedis()

	email := "ok@example.com"
	scene := "register"
	code := "654321"

	key := emailVerifyCodePrefix + scene + ":" + email
	store.RDB.Set(context.Background(), key, code, emailVerifyCodeTTL)

	// 先失败一次，再用正确码
	VerifyEmailCode(email, scene, "000000")
	if !VerifyEmailCode(email, scene, code) {
		t.Fatal("correct code should pass")
	}

	// 验证码与失败计数均已清除
	if v, _ := store.RDB.Get(context.Background(), key).Result(); v != "" {
		t.Fatal("code should be deleted after success")
	}
	attemptKey := emailVerifyAttemptPrefix + scene + ":" + email
	if v, _ := store.RDB.Get(context.Background(), attemptKey).Result(); v != "" {
		t.Fatal("attempt counter should be deleted after success")
	}
}
