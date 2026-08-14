package service

import (
	"github.com/mojocn/base64Captcha"
)

// GenerateCaptcha 生成图形验证码
func GenerateCaptcha() (string, string, error) {
	// 配置验证码的参数：高度、宽度、长度、干扰线数量、干扰点比率
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	captcha := base64Captcha.NewCaptcha(driver, currentCaptchaStore())
	id, b64s, _, err := captcha.Generate()
	return id, b64s, err
}

// VerifyCaptcha 验证图形验证码
func VerifyCaptcha(id string, verifyValue string) bool {
	// true 表示验证后即清除该验证码
	return currentCaptchaStore().Verify(id, verifyValue, true)
}
