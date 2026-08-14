package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestNormalizeVoiceModelsSettings(t *testing.T) {
	t.Run("trims, assigns id and sort", func(t *testing.T) {
		in := systemsetting.VoiceModelsSettings{Options: []systemsetting.VoiceModelOption{
			{Label: "  豆包  ", Provider: " doubao_realtime ", Model: " doubao-realtime ", Endpoint: " wss://host/v3 ", Enabled: true, Sort: 9},
		}}
		out, err := normalizeVoiceModelsSettings(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		opt := out.Options[0]
		if opt.Label != "豆包" || opt.Provider != "doubao_realtime" || opt.Model != "doubao-realtime" || opt.Endpoint != "wss://host/v3" {
			t.Fatalf("fields not trimmed: %+v", opt)
		}
		if opt.ID == "" {
			t.Fatal("id should be auto-generated")
		}
		if opt.Sort != 0 {
			t.Fatalf("sort should be reindexed to 0, got %d", opt.Sort)
		}
	})

	t.Run("rejects unsupported provider", func(t *testing.T) {
		in := systemsetting.VoiceModelsSettings{Options: []systemsetting.VoiceModelOption{
			{Label: "X", Provider: "unknown_realtime", Model: "m", Endpoint: "wss://h/x"},
		}}
		if _, err := normalizeVoiceModelsSettings(in); err == nil {
			t.Fatal("expected error for unsupported provider")
		}
	})

	t.Run("rejects empty label/model and bad endpoint", func(t *testing.T) {
		cases := []systemsetting.VoiceModelOption{
			{Label: "", Provider: "openai_realtime", Model: "m", Endpoint: "wss://h/x"},
			{Label: "X", Provider: "openai_realtime", Model: "", Endpoint: "wss://h/x"},
			{Label: "X", Provider: "openai_realtime", Model: "m", Endpoint: "https://h/x"},
			{Label: "X", Provider: "openai_realtime", Model: "m", Endpoint: ""},
		}
		for i, c := range cases {
			if _, err := normalizeVoiceModelsSettings(systemsetting.VoiceModelsSettings{Options: []systemsetting.VoiceModelOption{c}}); err == nil {
				t.Fatalf("case %d: expected validation error", i)
			}
		}
	})

	t.Run("rejects duplicate id", func(t *testing.T) {
		in := systemsetting.VoiceModelsSettings{Options: []systemsetting.VoiceModelOption{
			{ID: "dup", Label: "A", Provider: "openai_realtime", Model: "m1", Endpoint: "wss://h/1"},
			{ID: "dup", Label: "B", Provider: "openai_realtime", Model: "m2", Endpoint: "wss://h/2"},
		}}
		if _, err := normalizeVoiceModelsSettings(in); err == nil {
			t.Fatal("expected error for duplicate id")
		}
	})
}
