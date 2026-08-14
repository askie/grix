// SigV4 minimal implementation for AWS SNS Publish.
// 不引入 aws-sdk-go-v2（依赖太重），仅自签名 + net/http POST。
// 参考：https://docs.aws.amazon.com/general/latest/gr/sigv4_signing.html
package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	awsAlgorithm        = "AWS4-HMAC-SHA256"
	awsRequestSuffix    = "aws4_request"
	awsAmzDateFormat    = "20060102T150405Z"
	awsShortDateFormat  = "20060102"
)

// signAWSV4Form 给 form-encoded POST 请求加 SigV4 签名 header。
// 调用前 req.Body 必须是 form 字节流，且 Content-Type 已设。
func signAWSV4Form(req *http.Request, body []byte, accessKeyID, secretKey, region, service string, now time.Time) {
	amzDate := now.UTC().Format(awsAmzDateFormat)
	shortDate := now.UTC().Format(awsShortDateFormat)

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Host", req.URL.Host)

	payloadHash := hashSHA256Hex(body)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalHeaders, signedHeaders := buildCanonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/%s/%s", shortDate, region, service, awsRequestSuffix)
	stringToSign := strings.Join([]string{
		awsAlgorithm,
		amzDate,
		scope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(secretKey, shortDate, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	auth := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		awsAlgorithm, accessKeyID, scope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

// buildCanonicalHeaders 返回规范化 header 行 + 签名 header 列表。
// 仅包含 host / x-amz-date / x-amz-content-sha256 + Content-Type（保证 sigv4 验证）。
func buildCanonicalHeaders(req *http.Request) (string, string) {
	want := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		want = append(want, "content-type")
	}
	sort.Strings(want)
	var b strings.Builder
	for _, h := range want {
		v := req.Header.Get(h)
		if h == "host" {
			v = req.URL.Host
		}
		b.WriteString(h)
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(v))
		b.WriteString("\n")
	}
	return b.String(), strings.Join(want, ";")
}

func deriveSigningKey(secret, shortDate, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte(awsRequestSuffix))
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

func hashSHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
