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
	"testing"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/config"
)

func TestNewWorker(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address:  "localhost:6379",
		Password: "",
		DB:       0,
	}
	queueCfg := config.QueueConfig{
		Concurrency: 5,
	}

	w := NewWorker(redisCfg, queueCfg)
	if w == nil {
		t.Fatal("NewWorker() returned nil")
	}
	if w.server == nil {
		t.Error("worker.server is nil")
	}
	if w.mux == nil {
		t.Error("worker.mux is nil")
	}
}

func TestNewWorker_DefaultConcurrency(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address: "localhost:6379",
	}
	queueCfg := config.QueueConfig{
		Concurrency: 0,
	}

	w := NewWorker(redisCfg, queueCfg)
	if w == nil {
		t.Fatal("NewWorker() returned nil")
	}
}

func TestNewWorker_WithPassword(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address:  "localhost:6379",
		Password: "secret",
		DB:       2,
	}
	queueCfg := config.QueueConfig{
		Concurrency: 3,
	}

	w := NewWorker(redisCfg, queueCfg)
	if w == nil {
		t.Fatal("NewWorker() returned nil")
	}
}

// fakeHandler implements asynq.Handler for testing RegisterHandler.
type fakeHandler struct {
	called bool
}

func (h *fakeHandler) ProcessTask(_ context.Context, _ *asynq.Task) error {
	h.called = true
	return nil
}

func TestWorker_RegisterHandler(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address: "localhost:6379",
	}
	queueCfg := config.QueueConfig{
		Concurrency: 1,
	}

	w := NewWorker(redisCfg, queueCfg)

	// RegisterHandler should not panic
	handler := &fakeHandler{}
	w.RegisterHandler(TypeWebhookProcess, handler)

	// Register a second handler pattern
	w.RegisterHandler("another:task", &fakeHandler{})
}

func TestWorker_RegisterMultipleHandlers(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address: "localhost:6379",
	}
	queueCfg := config.QueueConfig{
		Concurrency: 10,
	}

	w := NewWorker(redisCfg, queueCfg)

	patterns := []string{
		TypeWebhookProcess,
		"task:cleanup",
		"task:notify",
	}

	for _, p := range patterns {
		w.RegisterHandler(p, &fakeHandler{})
	}
}

func TestWorker_Shutdown(t *testing.T) {
	redisCfg := config.RedisConfig{
		Address: "localhost:6379",
	}
	queueCfg := config.QueueConfig{
		Concurrency: 1,
	}

	w := NewWorker(redisCfg, queueCfg)
	// Shutdown on a worker that hasn't started Run should not panic
	w.Shutdown()
}
