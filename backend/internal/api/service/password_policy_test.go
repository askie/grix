package service

import (
	"strings"
	"testing"
)

func TestValidateUserPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid letters and digits", password: "Password123", wantErr: false},
		{name: "too short", password: "a1b2c3", wantErr: true},
		{name: "no digit", password: "PasswordOnly", wantErr: true},
		{name: "no letter", password: "1234567890", wantErr: true},
		{name: "longer than bcrypt limit", password: strings.Repeat("A1", 37), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUserPassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUserPassword() error = %v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr && err != ErrUserPasswordPolicy {
				t.Fatalf("expected ErrUserPasswordPolicy, got %v", err)
			}
		})
	}
}
