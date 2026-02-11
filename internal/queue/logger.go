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
	"fmt"
	"log/slog"
)

// slogAdapter bridges asynq's Logger interface to slog.
type slogAdapter struct {
	logger *slog.Logger
}

// newSlogAdapter creates a new slog adapter for asynq.
func newSlogAdapter(logger *slog.Logger) *slogAdapter {
	return &slogAdapter{logger: logger.With("component", "asynq")}
}

func (l *slogAdapter) Debug(args ...interface{}) {
	l.logger.Debug(fmt.Sprint(args...))
}

func (l *slogAdapter) Info(args ...interface{}) {
	l.logger.Info(fmt.Sprint(args...))
}

func (l *slogAdapter) Warn(args ...interface{}) {
	l.logger.Warn(fmt.Sprint(args...))
}

func (l *slogAdapter) Error(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}

func (l *slogAdapter) Fatal(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}
