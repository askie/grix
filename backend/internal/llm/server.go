package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/metrics"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
	"github.com/nats-io/nats.go"
)

type Server struct {
	orchestrator *Orchestrator
	port         int
	srv          *http.Server
}

const (
	aiRequestQueueGroup         = "llm-ai-request-v1"
	aiRequestDurable            = "llm-ai-request-v1"
	aiRequestAckWait            = 10 * time.Minute
	aiRequestInProgressInterval = 20 * time.Second
)

func NewServer(port int, providers map[string]provider.Provider) *Server {
	return &Server{
		orchestrator: NewOrchestrator(providers),
		port:         port,
	}
}

func (s *Server) Start(ctx context.Context) error {
	// Start NATS consumer for AI requests
	go s.startNATSConsumer(ctx)

	// Start embedding worker
	go s.orchestrator.StartEmbeddingWorker(ctx)

	// HTTP health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(version.Get())
	})
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		dbOk, redisOk, natsOk := store.ReadyCheck(3 * time.Second)
		if dbOk && redisOk && natsOk {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"db":%t,"redis":%t,"nats":%t}`, dbOk, redisOk, natsOk)
	})

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	logger.L.Infof("llm server starting on port %d", s.port)
	return s.srv.ListenAndServe()
}

func (s *Server) startNATSConsumer(ctx context.Context) {
	handler := func(msg *nats.Msg) {
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(aiRequestInProgressInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := msg.InProgress(); err != nil {
						logger.L.Warnf("ai request in-progress ack error: %v", err)
					}
				case <-done:
					return
				}
			}
		}()
		defer close(done)

		var req AIRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			logger.L.Errorf("ai request unmarshal error: %v", err)
			if err := msg.Ack(); err != nil {
				logger.L.Warnf("ai request ack error (unmarshal): %v", err)
			}
			return
		}

		s.orchestrator.HandleRequest(ctx, &req)
		if err := msg.Ack(); err != nil {
			logger.L.Warnf("ai request ack error: %v", err)
		}
	}

	sub, err := store.JS.QueueSubscribe("ai.request.*", aiRequestQueueGroup, handler,
		nats.ManualAck(),
		nats.Durable(aiRequestDurable),
		nats.DeliverNew(),
		nats.AckWait(aiRequestAckWait),
		nats.MaxDeliver(5),
	)

	if err != nil {
		logger.L.Fatalf("failed to subscribe to ai.request.*: %v", err)
	}
	if info, infoErr := sub.ConsumerInfo(); infoErr != nil {
		logger.L.Warnf("load ai.request consumer info error: %v", infoErr)
	} else {
		logger.L.Infof(
			"ai.request consumer ready stream=%s durable=%s deliver_policy=%s ack_wait=%s pending=%d ack_pending=%d redelivered=%d",
			info.Stream,
			info.Name,
			info.Config.DeliverPolicy,
			info.Config.AckWait,
			info.NumPending,
			info.NumAckPending,
			info.NumRedelivered,
		)
	}

	<-ctx.Done()
	if err := sub.Unsubscribe(); err != nil {
		logger.L.Warnf("ai.request consumer unsubscribe error: %v", err)
	}
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}
