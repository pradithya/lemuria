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
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/models"
)

// TypeWebhookProcess is the task type for processing webhook events.
const TypeWebhookProcess = "webhook:process"

// WebhookTaskPayload is the payload stored in each webhook task.
type WebhookTaskPayload struct {
	DeliveryID string          `json:"deliveryID"`
	Event      *models.PREvent `json:"event"`
}

// NewWebhookTask creates an asynq.Task for processing a webhook event.
// The delivery ID is used as the task ID for deduplication.
func NewWebhookTask(deliveryID string, event *models.PREvent) (*asynq.Task, error) {
	payload, err := json.Marshal(WebhookTaskPayload{
		DeliveryID: deliveryID,
		Event:      event,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling webhook task payload: %w", err)
	}

	return asynq.NewTask(
		TypeWebhookProcess,
		payload,
		asynq.TaskID(deliveryID),
		asynq.MaxRetry(5),
		asynq.Queue("webhooks"),
		asynq.Retention(24*time.Hour),
	), nil
}
