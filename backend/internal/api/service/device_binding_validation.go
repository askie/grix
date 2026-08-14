package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
)

var errInvalidDeviceBinding = errors.New("invalid device binding")

type normalizedDeviceBinding struct {
	platform    string
	pushEnv     string
	deviceToken string
	deviceID    string
}

func IsInvalidDeviceBinding(err error) bool {
	return errors.Is(err, errInvalidDeviceBinding)
}

func normalizeDeviceBinding(platform, pushEnv, deviceToken, deviceID string) (normalizedDeviceBinding, error) {
	binding := normalizedDeviceBinding{
		platform:    strings.TrimSpace(platform),
		pushEnv:     strings.TrimSpace(pushEnv),
		deviceToken: strings.TrimSpace(deviceToken),
		deviceID:    strings.TrimSpace(deviceID),
	}

	if binding.platform == "" {
		return normalizedDeviceBinding{}, invalidDeviceBinding("platform is required")
	}
	if binding.pushEnv == "" {
		return normalizedDeviceBinding{}, invalidDeviceBinding("push_env is required")
	}
	if binding.deviceToken == "" {
		return normalizedDeviceBinding{}, invalidDeviceBinding("device_token is required")
	}
	if binding.deviceID == "" {
		return normalizedDeviceBinding{}, invalidDeviceBinding("device_id is required")
	}

	switch binding.platform {
	case model.DevicePlatformIOS:
		if binding.pushEnv != model.DevicePushEnvAPNsSandbox && binding.pushEnv != model.DevicePushEnvAPNsProduction {
			return normalizedDeviceBinding{}, invalidDeviceBinding("ios push_env must be apns_sandbox or apns_production")
		}
	case model.DevicePlatformAndroidFCM, model.DevicePlatformAndroidJPush, model.DevicePlatformWebPush:
		if binding.pushEnv != model.DevicePushEnvDefault {
			return normalizedDeviceBinding{}, invalidDeviceBinding("non-ios push_env must be default")
		}
	default:
		if !model.IsAndroidVendorPlatform(binding.platform) {
			return normalizedDeviceBinding{}, invalidDeviceBinding("unsupported platform")
		}
		if binding.pushEnv != model.DevicePushEnvDefault {
			return normalizedDeviceBinding{}, invalidDeviceBinding("non-ios push_env must be default")
		}
	}

	return binding, nil
}

func invalidDeviceBinding(reason string) error {
	return fmt.Errorf("%w: %s", errInvalidDeviceBinding, reason)
}
