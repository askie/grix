package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	phoneRegistrationTicketPrefix = "auth:phone:registration_ticket:"
	phoneRegistrationTicketTTL    = 5 * time.Minute
)

func issuePhoneRegistrationTicket(ctx context.Context, phone string) (string, error) {
	if store.RDB == nil {
		return "", ErrAuthServiceUnavailable
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", ErrAuthServiceUnavailable
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	ok, err := store.RDB.SetNX(ctx, phoneRegistrationTicketPrefix+ticket, phone, phoneRegistrationTicketTTL).Result()
	if err != nil || !ok {
		return "", ErrAuthServiceUnavailable
	}
	return ticket, nil
}

func consumePhoneRegistrationTicket(ctx context.Context, ticket, phone string) (bool, error) {
	if store.RDB == nil {
		return false, ErrAuthServiceUnavailable
	}
	storedPhone, err := store.RDB.GetDel(ctx, phoneRegistrationTicketPrefix+ticket).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, ErrAuthServiceUnavailable
	}
	return storedPhone == phone, nil
}
