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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func counterValue(cv *prometheus.CounterVec, labels ...string) float64 {
	var m dto.Metric
	if err := cv.WithLabelValues(labels...).Write(&m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

func histogramCount(hv *prometheus.HistogramVec, labels ...string) uint64 {
	var m dto.Metric
	observer := hv.WithLabelValues(labels...)
	if err := observer.(prometheus.Metric).Write(&m); err != nil {
		return 0
	}
	return m.GetHistogram().GetSampleCount()
}

func TestRecordWebhookRequest(t *testing.T) {
	before := counterValue(WebhookRequestsTotal, "github", "push", "accepted")
	RecordWebhookRequest("github", "push", "accepted")
	after := counterValue(WebhookRequestsTotal, "github", "push", "accepted")

	if after != before+1 {
		t.Errorf("expected counter to increment by 1, got delta=%f", after-before)
	}
}

func TestObserveWebhookProcessingDuration(t *testing.T) {
	before := histogramCount(WebhookProcessingDuration, "github", "push")
	ObserveWebhookProcessingDuration("github", "push", time.Now().Add(-100*time.Millisecond))
	after := histogramCount(WebhookProcessingDuration, "github", "push")

	if after != before+1 {
		t.Errorf("expected histogram sample count to increment by 1, got delta=%d", after-before)
	}
}

func TestRecordCommandExecution(t *testing.T) {
	before := counterValue(CommandExecutionsTotal, "plan", "success")
	RecordCommandExecution("plan", "success")
	after := counterValue(CommandExecutionsTotal, "plan", "success")

	if after != before+1 {
		t.Errorf("expected counter to increment by 1, got delta=%f", after-before)
	}
}

func TestObserveCommandDuration(t *testing.T) {
	before := histogramCount(CommandDuration, "sync")
	ObserveCommandDuration("sync", time.Now().Add(-50*time.Millisecond))
	after := histogramCount(CommandDuration, "sync")

	if after != before+1 {
		t.Errorf("expected histogram sample count to increment by 1, got delta=%d", after-before)
	}
}

func TestRecordArgoCDRequest(t *testing.T) {
	before := counterValue(ArgoCDRequestsTotal, "GET", "/api/v1/applications", "200")
	RecordArgoCDRequest("GET", "/api/v1/applications", 200)
	after := counterValue(ArgoCDRequestsTotal, "GET", "/api/v1/applications", "200")

	if after != before+1 {
		t.Errorf("expected counter to increment by 1, got delta=%f", after-before)
	}
}

func TestObserveArgoCDRequestDuration(t *testing.T) {
	before := histogramCount(ArgoCDRequestDuration, "GET")
	ObserveArgoCDRequestDuration("GET", time.Now().Add(-10*time.Millisecond))
	after := histogramCount(ArgoCDRequestDuration, "GET")

	if after != before+1 {
		t.Errorf("expected histogram sample count to increment by 1, got delta=%d", after-before)
	}
}

func TestRecordLockOperation(t *testing.T) {
	before := counterValue(LockOperationsTotal, "lock", "success")
	RecordLockOperation("lock", "success")
	after := counterValue(LockOperationsTotal, "lock", "success")

	if after != before+1 {
		t.Errorf("expected counter to increment by 1, got delta=%f", after-before)
	}
}

func TestRecordApplicationsDetected(t *testing.T) {
	before := counterValue(PlanApplicationsDetectedTotal, "new")
	RecordApplicationsDetected("new", 3)
	after := counterValue(PlanApplicationsDetectedTotal, "new")

	if after != before+3 {
		t.Errorf("expected counter to increment by 3, got delta=%f", after-before)
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/api/version", "/api/version"},
		{"/api/v1/applications", "/api/v1/applications"},
		{"/api/v1/applications/my-app", "/api/v1/applications"},
		{"/api/v1/applications/my-app/resource", "/api/v1/applications"},
		{"/api/v1/applicationsets/my-set", "/api/v1/applicationsets"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizePath(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
