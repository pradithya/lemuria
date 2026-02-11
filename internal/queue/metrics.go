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
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/org/lemuria/internal/config"
)

const metricsNamespace = "lemuria"
const metricsSubsystem = "queue"

// QueueCollector implements prometheus.Collector to expose asynq queue metrics.
type QueueCollector struct {
	inspector *asynq.Inspector

	// Gauges
	size      *prometheus.Desc
	pending   *prometheus.Desc
	active    *prometheus.Desc
	scheduled *prometheus.Desc
	retry     *prometheus.Desc
	archived  *prometheus.Desc
	completed *prometheus.Desc
	latency   *prometheus.Desc

	// Counters (exposed as gauges since they come from asynq snapshots)
	processedTotal *prometheus.Desc
	failedTotal    *prometheus.Desc
}

// NewQueueCollector creates a Prometheus collector backed by an asynq Inspector.
func NewQueueCollector(redisCfg config.RedisConfig) *QueueCollector {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     redisCfg.Address,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	})

	labels := []string{"queue"}

	return &QueueCollector{
		inspector: inspector,
		size: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "size"),
			"Total number of tasks in the queue",
			labels, nil,
		),
		pending: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "pending"),
			"Number of pending tasks",
			labels, nil,
		),
		active: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "active"),
			"Number of active tasks being processed",
			labels, nil,
		),
		scheduled: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "scheduled"),
			"Number of scheduled tasks",
			labels, nil,
		),
		retry: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "retry"),
			"Number of tasks in retry state",
			labels, nil,
		),
		archived: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "archived"),
			"Number of archived (dead) tasks",
			labels, nil,
		),
		completed: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "completed"),
			"Number of completed tasks retained",
			labels, nil,
		),
		latency: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "latency_seconds"),
			"Latency of the queue in seconds (oldest pending task age)",
			labels, nil,
		),
		processedTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "processed_total"),
			"Total number of tasks processed (cumulative)",
			labels, nil,
		),
		failedTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, "failed_total"),
			"Total number of tasks failed (cumulative)",
			labels, nil,
		),
	}
}

// Describe sends the descriptor of each metric to the provided channel.
func (c *QueueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.size
	ch <- c.pending
	ch <- c.active
	ch <- c.scheduled
	ch <- c.retry
	ch <- c.archived
	ch <- c.completed
	ch <- c.latency
	ch <- c.processedTotal
	ch <- c.failedTotal
}

// Collect fetches queue info from asynq and sends metrics to the channel.
func (c *QueueCollector) Collect(ch chan<- prometheus.Metric) {
	queues, err := c.inspector.Queues()
	if err != nil {
		slog.Warn("failed to list queues for metrics", "error", err)
		return
	}

	for _, q := range queues {
		info, err := c.inspector.GetQueueInfo(q)
		if err != nil {
			slog.Warn("failed to get queue info for metrics", "queue", q, "error", err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(c.size, prometheus.GaugeValue, float64(info.Size), q)
		ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(info.Pending), q)
		ch <- prometheus.MustNewConstMetric(c.active, prometheus.GaugeValue, float64(info.Active), q)
		ch <- prometheus.MustNewConstMetric(c.scheduled, prometheus.GaugeValue, float64(info.Scheduled), q)
		ch <- prometheus.MustNewConstMetric(c.retry, prometheus.GaugeValue, float64(info.Retry), q)
		ch <- prometheus.MustNewConstMetric(c.archived, prometheus.GaugeValue, float64(info.Archived), q)
		ch <- prometheus.MustNewConstMetric(c.completed, prometheus.GaugeValue, float64(info.Completed), q)
		ch <- prometheus.MustNewConstMetric(c.latency, prometheus.GaugeValue, info.Latency.Seconds(), q)
		ch <- prometheus.MustNewConstMetric(c.processedTotal, prometheus.GaugeValue, float64(info.ProcessedTotal), q)
		ch <- prometheus.MustNewConstMetric(c.failedTotal, prometheus.GaugeValue, float64(info.FailedTotal), q)
	}
}

// Close releases resources held by the collector.
func (c *QueueCollector) Close() error {
	return c.inspector.Close()
}
