// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/config"
)

// Worker wraps an asynq server for processing queued tasks.
type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

// NewWorker creates a new queue worker.
func NewWorker(redisCfg config.RedisConfig, queueCfg config.QueueConfig) *Worker {
	redisOpt := asynq.RedisClientOpt{
		Addr:     redisCfg.Address,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	}

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     queueCfg.Concurrency,
		ShutdownTimeout: 5 * time.Minute,
		Queues: map[string]int{
			"webhooks": 10,
		},
		Logger: newSlogAdapter(),
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			slog.Error("task failed",
				"type", task.Type(),
				"error", err,
			)
		}),
	})

	return &Worker{
		server: srv,
		mux:    asynq.NewServeMux(),
	}
}

// RegisterHandler registers a task handler for the given pattern.
func (w *Worker) RegisterHandler(pattern string, handler asynq.Handler) {
	w.mux.Handle(pattern, handler)
}

// Run starts the worker and blocks until SIGTERM or SIGINT.
func (w *Worker) Run() error {
	slog.Info("starting queue worker")
	return w.server.Run(w.mux)
}

// Shutdown gracefully stops the worker.
func (w *Worker) Shutdown() {
	w.server.Shutdown()
}
