package ipgeo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupKnownIPs(t *testing.T) {
	// 114DNS（江苏南京），归属地应含"中国"
	loc := Lookup("114.114.114.114")
	assert.NotEmpty(t, loc)
	assert.Contains(t, loc, "中国")

	// Google Public DNS，非中国
	loc = Lookup("8.8.8.8")
	assert.NotEmpty(t, loc)
	assert.NotContains(t, loc, "中国")
}

func TestLookupSkipsPrivateAndInvalid(t *testing.T) {
	assert.Equal(t, "", Lookup("192.168.1.1"))
	assert.Equal(t, "", Lookup("127.0.0.1"))
	assert.Equal(t, "", Lookup("10.0.0.8"))
	assert.Equal(t, "", Lookup("not-an-ip"))
	assert.Equal(t, "", Lookup(""))
	// IPv6 暂不支持，返回空而非报错
	assert.Equal(t, "", Lookup("2001:db8::1"))
}

func TestLookupConcurrent(t *testing.T) {
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				_ = Lookup("114.114.114.114")
				_ = Lookup("8.8.8.8")
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
