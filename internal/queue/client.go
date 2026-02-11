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
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// Client wraps an asynq client for enqueuing webhook tasks.
type Client struct {
	client *asynq.Client
}

// NewClient creates a new queue client from the existing Redis config.
func NewClient(cfg config.RedisConfig) *Client {
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	return &Client{
		client: asynq.NewClient(redisOpt),
	}
}

// EnqueueWebhook creates and enqueues a webhook processing task.
// Duplicate tasks (same delivery ID) are treated as success.
func (c *Client) EnqueueWebhook(deliveryID string, event *models.PREvent) error {
	task, err := NewWebhookTask(deliveryID, event)
	if err != nil {
		return fmt.Errorf("creating webhook task: %w", err)
	}

	info, err := c.client.Enqueue(task)
	if err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) {
			slog.Debug("duplicate task ignored", "delivery_id", deliveryID)
			return nil
		}
		return fmt.Errorf("enqueuing webhook task: %w", err)
	}

	slog.Info("enqueued webhook task",
		"task_id", info.ID,
		"queue", info.Queue,
		"delivery_id", deliveryID,
		"repo", event.Repo.FullName,
		"pr", event.PR.Number,
	)
	return nil
}

// Close releases the client's resources.
func (c *Client) Close() error {
	return c.client.Close()
}
