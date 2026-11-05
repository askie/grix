package push

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestInstantMessageFlag(t *testing.T) {
	on := systemsetting.PushChannelSettings{HuaweiIMCategoryEnabled: true}
	off := systemsetting.PushChannelSettings{}

	cases := []struct {
		name       string
		senderType int16
		isAI       bool
		channels   systemsetting.PushChannelSettings
		want       bool
	}{
		{"human chat with the switch on", senderTypeHuman, false, on, true},
		{"switch off keeps the vendor default classification", senderTypeHuman, false, off, false},
		{"agent session is not IM", senderTypeHuman, true, on, false},
		{"agent sender is not IM", 2, false, on, false},
		{"system sender is not IM", 3, false, on, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, instantMessageFlag(tc.senderType, tc.isAI, tc.channels))
		})
	}
}
