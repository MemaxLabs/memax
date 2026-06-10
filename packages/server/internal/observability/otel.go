package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type SetupResult struct {
	Enabled  bool
	Shutdown func(context.Context) error
}

func Setup(ctx context.Context, defaultServiceName string) (SetupResult, error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return SetupResult{
			Enabled: false,
			Shutdown: func(context.Context) error {
				return nil
			},
		}, nil
	}

	protocol, err := otlpProtocol()
	if err != nil {
		return SetupResult{}, err
	}

	res, err := newResource(ctx, defaultServiceName)
	if err != nil {
		return SetupResult{}, err
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return SetupResult{}, err
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(15*time.Second))),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	slog.Info(
		"otel observability enabled",
		"endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		"protocol", protocol,
		"service_name", defaultServiceName,
		"environment", environmentName(),
	)

	return SetupResult{
		Enabled: true,
		Shutdown: func(ctx context.Context) error {
			return errors.Join(
				traceProvider.Shutdown(ctx),
				meterProvider.Shutdown(ctx),
			)
		},
	}, nil
}

func newResource(ctx context.Context, defaultServiceName string) (*resource.Resource, error) {
	defaultResource, err := resource.New(
		ctx,
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(defaultServiceName),
			semconv.ServiceNamespace("memax"),
			attribute.String("deployment.environment", environmentName()),
		),
	)
	if err != nil {
		return nil, err
	}

	envResource, err := resource.New(ctx, resource.WithFromEnv())
	if err != nil {
		return nil, err
	}

	merged, err := resource.Merge(defaultResource, envResource)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func otlpProtocol() (string, error) {
	value := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	if value == "" {
		return "http/protobuf", nil
	}
	if value != "http/protobuf" {
		return "", fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q: memax server only supports http/protobuf", value)
	}
	return value, nil
}

func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
