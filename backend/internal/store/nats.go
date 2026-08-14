package store

import (
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
)

var NC *nats.Conn
var JS nats.JetStreamContext

// Durable terminal notification retries normally reclaim after 30 seconds.
// Keep the server-side Nats-Msg-Id cache far beyond that recovery window so a
// publish-before-ledger-complete crash is absorbed during rolling outages too.
const jetStreamDuplicateWindow = 24 * time.Hour

// streamSubjects is the full set of JetStream subjects the platform uses. Any
// service that runs InitNATS reconciles the shared stream against this list, so
// adding a subject here and redeploying one service is enough to register it.
var streamSubjects = []string{
	"im.push.offline.*",
	"ai.embedding.generate",
	"ai.request.*",
	"ai.embedding.dead_letter",
	"agent.notification.events",
	"agent.notification.tts",
	"im.reach.event",
	"pay.order.*",
	"pay.refund.*",
	// 对账告警（pay.reconcile.failed）。漏了它，Service.publish 每轮对账都拿到
	// "no response from stream"，告警发不出去还刷一屏错误日志——而对账失败正是
	// "凭证配错 / 通道整体不可用" 这类问题唯一的自动信号，不能哑掉。
	"pay.reconcile.*",
}

func InitNATS(cfg config.NATSConfig) {
	var err error
	NC, err = nats.Connect(cfg.URL)
	if err != nil {
		logger.L.Fatalf("failed to connect nats: %v", err)
	}
	JS, err = NC.JetStream()
	if err != nil {
		logger.L.Fatalf("failed to init jetstream: %v", err)
	}

	// Create the stream if absent, otherwise reconcile its subjects. AddStream
	// is a no-op on an existing stream, so new subjects must be added via
	// UpdateStream — preserving every other live setting.
	info, infoErr := JS.StreamInfo(cfg.StreamName)
	if infoErr != nil {
		if _, addErr := JS.AddStream(&nats.StreamConfig{
			Name:       cfg.StreamName,
			Subjects:   streamSubjects,
			Storage:    nats.FileStorage,
			Duplicates: jetStreamDuplicateWindow,
		}); addErr != nil {
			logger.L.Warnf("jetstream add stream: %v", addErr)
		}
	} else {
		updated := info.Config
		changed := false
		for _, subj := range streamSubjects {
			if !containsString(updated.Subjects, subj) {
				updated.Subjects = append(updated.Subjects, subj)
				changed = true
			}
		}
		if updated.Duplicates < jetStreamDuplicateWindow {
			updated.Duplicates = jetStreamDuplicateWindow
			changed = true
		}
		if changed {
			if _, upErr := JS.UpdateStream(&updated); upErr != nil {
				logger.L.Warnf("jetstream update stream subjects: %v", upErr)
			} else {
				logger.L.Infof("jetstream stream %s subjects reconciled: %v", cfg.StreamName, updated.Subjects)
			}
		}
	}
	logger.L.Info("nats connected")
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
