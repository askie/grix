package service

import (
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

func ValidateAdminPassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("密码长度至少 12 位")
	}

	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("密码必须同时包含大写字母、小写字母和数字")
	}
	return nil
}

func HashAdminPassword(password string) (string, error) {
	if err := ValidateAdminPassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
