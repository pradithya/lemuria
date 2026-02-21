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
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func startMiniredisForMetrics(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr
}

func TestNewQueueCollector(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	collector := NewQueueCollector(cfg)
	if collector == nil {
		t.Fatal("NewQueueCollector() returned nil")
	}
	if collector.inspector == nil {
		t.Fatal("collector.inspector is nil")
	}
}

func TestNewQueueCollector_WithPassword(t *testing.T) {
	mr := startMiniredisForMetrics(t)
	mr.RequireAuth("pass123")

	cfg := config.RedisConfig{
		Address:  mr.Addr(),
		Password: "pass123",
		DB:       2,
	}

	collector := NewQueueCollector(cfg)
	if collector == nil {
		t.Fatal("NewQueueCollector() returned nil")
	}
	defer func() { _ = collector.Close() }()
}

func TestQueueCollector_Describe(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	collector := NewQueueCollector(cfg)
	defer func() { _ = collector.Close() }()

	ch := make(chan *prometheus.Desc, 20)
	collector.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}

	// We expect exactly 10 metric descriptors
	expectedCount := 10
	if len(descs) != expectedCount {
		t.Fatalf("Describe() sent %d descriptors, want %d", len(descs), expectedCount)
	}

	// Verify each descriptor is non-nil
	for i, d := range descs {
		if d == nil {
			t.Errorf("descriptor %d is nil", i)
		}
	}
}

func TestQueueCollector_Collect_NoQueues(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	collector := NewQueueCollector(cfg)
	defer func() { _ = collector.Close() }()

	ch := make(chan prometheus.Metric, 100)
	collector.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	// No queues exist, so no metrics should be emitted
	if len(metrics) != 0 {
		t.Errorf("Collect() with no queues emitted %d metrics, want 0", len(metrics))
	}
}

func TestQueueCollector_Collect_WithQueue(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	// Enqueue a task so the "webhooks" queue exists
	client := NewClient(cfg)
	event := &models.PREvent{
		Provider:   models.VCSProviderGitHub,
		Type:       models.EventTypePullRequest,
		Action:     models.PRActionOpened,
		Repo:       models.RepoInfo{FullName: "org/repo"},
		PR:         models.PRInfo{Number: 1},
		ReceivedAt: time.Now(),
	}
	if err := client.EnqueueWebhook("metrics-test-1", event); err != nil {
		t.Fatalf("EnqueueWebhook() error = %v", err)
	}
	_ = client.Close()

	collector := NewQueueCollector(cfg)
	defer func() { _ = collector.Close() }()

	ch := make(chan prometheus.Metric, 100)
	collector.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	// With one queue, we expect 10 metrics (one per descriptor per queue)
	if len(metrics) != 10 {
		t.Errorf("Collect() with 1 queue emitted %d metrics, want 10", len(metrics))
	}

	// Verify each metric has a valid Desc
	for i, m := range metrics {
		if m.Desc() == nil {
			t.Errorf("metric %d has nil Desc", i)
		}
	}
}

func TestQueueCollector_Close(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	collector := NewQueueCollector(cfg)
	err := collector.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestQueueCollector_ImplementsCollector(t *testing.T) {
	mr := startMiniredisForMetrics(t)

	cfg := config.RedisConfig{
		Address: mr.Addr(),
	}

	collector := NewQueueCollector(cfg)
	defer func() { _ = collector.Close() }()

	// Verify the collector can be registered with Prometheus
	reg := prometheus.NewRegistry()
	err := reg.Register(collector)
	if err != nil {
		t.Fatalf("failed to register collector: %v", err)
	}
}

func TestQueueCollector_Collect_InvalidRedis(t *testing.T) {
	// Use an address that doesn't have a running Redis
	cfg := config.RedisConfig{
		Address: "localhost:1", // unlikely to have Redis running here
	}

	collector := NewQueueCollector(cfg)
	defer func() { _ = collector.Close() }()

	// Collect should not panic when Redis is unavailable
	ch := make(chan prometheus.Metric, 100)
	collector.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	// Should emit no metrics when Redis is unavailable
	if len(metrics) != 0 {
		t.Errorf("Collect() with unavailable Redis emitted %d metrics, want 0", len(metrics))
	}
}
