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

package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "lemuria"

// Webhook metrics.
var (
	WebhookRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "webhook",
		Name:      "requests_total",
		Help:      "Total number of webhook requests received.",
	}, []string{"provider", "event_type", "status"})

	WebhookProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "webhook",
		Name:      "processing_duration_seconds",
		Help:      "Duration of async webhook event processing in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"provider", "event_type"})
)

// Command metrics.
var (
	CommandExecutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "command",
		Name:      "executions_total",
		Help:      "Total number of command executions.",
	}, []string{"command", "status"})

	CommandDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "command",
		Name:      "duration_seconds",
		Help:      "Duration of command execution in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"command"})
)

// ArgoCD API metrics.
var (
	ArgoCDRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "argocd",
		Name:      "requests_total",
		Help:      "Total number of ArgoCD API requests.",
	}, []string{"method", "path", "status_code"})

	ArgoCDRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "argocd",
		Name:      "request_duration_seconds",
		Help:      "Duration of ArgoCD API requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})
)

// Lock metrics.
var (
	LockOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "lock",
		Name:      "operations_total",
		Help:      "Total number of lock operations.",
	}, []string{"operation", "status"})
)

// Plan metrics.
var (
	PlanApplicationsDetectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "plan",
		Name:      "applications_detected_total",
		Help:      "Total number of affected applications detected during planning.",
	}, []string{"change_type"})
)

// RecordWebhookRequest increments the webhook request counter.
func RecordWebhookRequest(provider, eventType, status string) {
	WebhookRequestsTotal.WithLabelValues(provider, eventType, status).Inc()
}

// ObserveWebhookProcessingDuration records the duration of webhook event processing.
func ObserveWebhookProcessingDuration(provider, eventType string, start time.Time) {
	WebhookProcessingDuration.WithLabelValues(provider, eventType).Observe(time.Since(start).Seconds())
}

// RecordCommandExecution increments the command execution counter.
func RecordCommandExecution(command, status string) {
	CommandExecutionsTotal.WithLabelValues(command, status).Inc()
}

// ObserveCommandDuration records the duration of a command execution.
func ObserveCommandDuration(command string, start time.Time) {
	CommandDuration.WithLabelValues(command).Observe(time.Since(start).Seconds())
}

// RecordArgoCDRequest increments the ArgoCD API request counter.
func RecordArgoCDRequest(method, path string, statusCode int) {
	ArgoCDRequestsTotal.WithLabelValues(method, path, strconv.Itoa(statusCode)).Inc()
}

// ObserveArgoCDRequestDuration records the duration of an ArgoCD API request.
func ObserveArgoCDRequestDuration(method string, start time.Time) {
	ArgoCDRequestDuration.WithLabelValues(method).Observe(time.Since(start).Seconds())
}

// RecordLockOperation increments the lock operation counter.
func RecordLockOperation(operation, status string) {
	LockOperationsTotal.WithLabelValues(operation, status).Inc()
}

// RecordApplicationsDetected increments the applications detected counter.
func RecordApplicationsDetected(changeType string, count int) {
	PlanApplicationsDetectedTotal.WithLabelValues(changeType).Add(float64(count))
}

// NormalizePath normalizes an ArgoCD API path for use as a metric label.
// It keeps the first two path segments (e.g., /api/v1) and replaces
// variable segments with placeholders.
func NormalizePath(path string) string {
	// Common ArgoCD API prefixes — keep the resource type, drop specific names.
	// Examples:
	//   /api/v1/applications/my-app -> /api/v1/applications
	//   /api/v1/applications -> /api/v1/applications
	//   /api/version -> /api/version
	segments := splitPath(path)
	if len(segments) <= 3 {
		return path
	}
	// Keep up to 3 segments (e.g., /api/v1/applications)
	return "/" + segments[0] + "/" + segments[1] + "/" + segments[2]
}

// splitPath splits a URL path into non-empty segments.
func splitPath(path string) []string {
	var segments []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if start < i {
				segments = append(segments, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		segments = append(segments, path[start:])
	}
	return segments
}
