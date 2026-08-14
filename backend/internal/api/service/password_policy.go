package service

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

const (
	minUserPasswordLength = 8
	maxUserPasswordBytes  = 72 // bcrypt only processes the first 72 bytes.
)

var ErrUserPasswordPolicy = errors.New("密码长度必须为8-72位，且同时包含字母和数字")

func ValidateUserPassword(password string) error {
	if utf8.RuneCountInString(password) < minUserPasswordLength {
		return ErrUserPasswordPolicy
	}
	if len(password) > maxUserPasswordBytes {
		return ErrUserPasswordPolicy
	}

	hasLetter := false
	hasDigit := false
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
			continue
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrUserPasswordPolicy
	}
	return nil
}
