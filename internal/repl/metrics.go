// Copyright 2024 The milliways Authors
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

package repl

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

var (
	replMetricsOnce   sync.Once
	replCostHist      metric.Float64Histogram
	replTokensInHist  metric.Int64Histogram
	replTokensOutHist metric.Int64Histogram
)

func initReplMetrics() {
	replMetricsOnce.Do(func() {
		m := otel.GetMeterProvider().Meter("milliways.repl")
		nm := noop.Meter{}
		var err error

		replCostHist, err = m.Float64Histogram("milliways.repl.dispatch.cost_usd",
			metric.WithDescription("Dispatch cost in USD"),
			metric.WithUnit("{USD}"))
		if err != nil {
			replCostHist, _ = nm.Float64Histogram("milliways.repl.dispatch.cost_usd")
		}

		replTokensInHist, err = m.Int64Histogram("milliways.repl.dispatch.tokens_input",
			metric.WithDescription("Input tokens per dispatch"),
			metric.WithUnit("{token}"))
		if err != nil {
			replTokensInHist, _ = nm.Int64Histogram("milliways.repl.dispatch.tokens_input")
		}

		replTokensOutHist, err = m.Int64Histogram("milliways.repl.dispatch.tokens_output",
			metric.WithDescription("Output tokens per dispatch"),
			metric.WithUnit("{token}"))
		if err != nil {
			replTokensOutHist, _ = nm.Int64Histogram("milliways.repl.dispatch.tokens_output")
		}
	})
}

// runnerAttr returns an OTel attribute identifying the runner.
func runnerAttr(runner string) attribute.KeyValue {
	return attribute.String("runner", runner)
}

// RecordDispatch records cost and token usage for a completed dispatch.
func RecordDispatch(ctx context.Context, runner string, costUSD float64, tokensIn, tokensOut int) {
	initReplMetrics()
	attrs := metric.WithAttributes(runnerAttr(runner))
	replCostHist.Record(ctx, costUSD, attrs)
	replTokensInHist.Record(ctx, int64(tokensIn), attrs)
	replTokensOutHist.Record(ctx, int64(tokensOut), attrs)
}
