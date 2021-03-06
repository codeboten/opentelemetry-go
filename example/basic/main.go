// Copyright The OpenTelemetry Authors
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

package main

import (
	"context"
	"log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout"
	"go.opentelemetry.io/otel/global"
	"go.opentelemetry.io/otel/label"
	"go.opentelemetry.io/otel/propagators"
	"go.opentelemetry.io/otel/sdk/metric/controller/push"
	"go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	fooKey     = label.Key("ex.com/foo")
	barKey     = label.Key("ex.com/bar")
	lemonsKey  = label.Key("ex.com/lemons")
	anotherKey = label.Key("ex.com/another")
)

func main() {
	exporter, err := stdout.NewExporter([]stdout.Option{
		stdout.WithQuantiles([]float64{0.5, 0.9, 0.99}),
		stdout.WithPrettyPrint(),
	}...)
	if err != nil {
		log.Fatalf("failed to initialize stdout export pipeline: %v", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	defer bsp.Shutdown()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(bsp))
	pusher := push.New(
		basic.New(
			simple.NewWithExactDistribution(),
			exporter,
		),
		exporter,
	)
	pusher.Start()
	defer pusher.Stop()
	global.SetTracerProvider(tp)
	global.SetMeterProvider(pusher.MeterProvider())

	// set global propagator to baggage (the default is no-op).
	global.SetTextMapPropagator(propagators.Baggage{})
	tracer := global.Tracer("ex.com/basic")
	meter := global.Meter("ex.com/basic")

	commonLabels := []label.KeyValue{lemonsKey.Int(10), label.String("A", "1"), label.String("B", "2"), label.String("C", "3")}

	oneMetricCB := func(_ context.Context, result otel.Float64ObserverResult) {
		result.Observe(1, commonLabels...)
	}
	_ = otel.Must(meter).NewFloat64ValueObserver("ex.com.one", oneMetricCB,
		otel.WithDescription("A ValueObserver set to 1.0"),
	)

	valuerecorderTwo := otel.Must(meter).NewFloat64ValueRecorder("ex.com.two")

	ctx := context.Background()
	ctx = otel.ContextWithBaggageValues(ctx, fooKey.String("foo1"), barKey.String("bar1"))

	valuerecorder := valuerecorderTwo.Bind(commonLabels...)
	defer valuerecorder.Unbind()

	err = func(ctx context.Context) error {
		var span otel.Span
		ctx, span = tracer.Start(ctx, "operation")
		defer span.End()

		span.AddEvent("Nice operation!", otel.WithAttributes(label.Int("bogons", 100)))
		span.SetAttributes(anotherKey.String("yes"))

		meter.RecordBatch(
			// Note: call-site variables added as context Entries:
			otel.ContextWithBaggageValues(ctx, anotherKey.String("xyz")),
			commonLabels,

			valuerecorderTwo.Measurement(2.0),
		)

		return func(ctx context.Context) error {
			var span otel.Span
			ctx, span = tracer.Start(ctx, "Sub operation...")
			defer span.End()

			span.SetAttributes(lemonsKey.String("five"))
			span.AddEvent("Sub span event")
			valuerecorder.Record(ctx, 1.3)

			return nil
		}(ctx)
	}(ctx)
	if err != nil {
		panic(err)
	}
}
