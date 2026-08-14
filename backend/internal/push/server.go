package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/askie/grix/backend/internal/metrics"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
)

type Server struct {
	worker     *Worker
	dispatcher *notification.Dispatcher
	port       int
	srv        *http.Server
}

func NewServer(
	port int,
	apnsSandbox *provider.APNsProvider,
	apnsProduction *provider.APNsProvider,
	fcm *provider.FCMProvider,
	jpush *provider.JPushProvider,
	webpush *provider.WebPushProvider,
	vendors map[string]provider.VendorSender,
) *Server {
	worker := NewWorker(apnsSandbox, apnsProduction, fcm, jpush, webpush, vendors)
	return &Server{
		worker:     worker,
		dispatcher: notification.NewDispatcher(worker.PushNotification),
		port:       port,
	}
}

func (s *Server) Start(ctx context.Context) error {
	// Start NATS worker
	go s.worker.Start(ctx)

	// Start the Agent-notification dispatcher (consumes agent.notification.events).
	if err := s.dispatcher.Start(ctx); err != nil {
		logger.L.Errorf("notification dispatcher start error: %v", err)
	}

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

	logger.L.Infof("push server starting on port %d", s.port)
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}
