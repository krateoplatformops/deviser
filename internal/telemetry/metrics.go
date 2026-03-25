package telemetry

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type Config struct {
	Enabled        bool
	ServiceName    string
	ExportInterval time.Duration
}

type Metrics struct {
	startupSuccess           metric.Int64Counter
	startupFailure           metric.Int64Counter
	dbConnectDuration        metric.Float64Histogram
	dbSchemaApplyDuration    metric.Float64Histogram
	dbSchemaApplyFailure     metric.Int64Counter
	partitionsEnsureDuration metric.Float64Histogram
	partitionsEnsureFailure  metric.Int64Counter
	partitionsMaintainDur    metric.Float64Histogram
	partitionsMaintainFail   metric.Int64Counter
	partitionsDroppedExpired metric.Int64Counter
	partitionsDroppedQuota   metric.Int64Counter
	partitionsBytesFreed     metric.Int64Counter
	resourcesPurgeDuration   metric.Float64Histogram
	resourcesPurgeRows       metric.Int64Counter
	resourcesPurgeFailure    metric.Int64Counter
	loopIterationSuccess     metric.Int64Counter
	loopIterationFailure     metric.Int64Counter

	partitionsEnsureDaysGauge metric.Int64ObservableGauge
	partitionsTotalGauge      metric.Int64ObservableGauge
	partitionsTotalBytesGauge metric.Int64ObservableGauge

	partitionsEnsureDays atomic.Int64
	partitionsTotal      atomic.Int64
	partitionsTotalBytes atomic.Int64
}

func Setup(ctx context.Context, log *slog.Logger, cfg Config) (*Metrics, func(context.Context) error, error) {
	if !cfg.Enabled {
		return nil, func(context.Context) error { return nil }, nil
	}

	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}

	resource, err := resource.Merge(resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName)))
	if err != nil {
		return nil, nil, err
	}

	options := []sdkmetric.PeriodicReaderOption{}
	if cfg.ExportInterval > 0 {
		options = append(options, sdkmetric.WithInterval(cfg.ExportInterval))
	}

	reader := sdkmetric.NewPeriodicReader(exporter, options...)
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(resource),
	)
	otel.SetMeterProvider(provider)

	meter := provider.Meter("github.com/krateoplatformops/deviser")
	metrics, err := newMetrics(meter)
	if err != nil {
		_ = provider.Shutdown(ctx)
		return nil, nil, err
	}

	log.Info("OpenTelemetry metrics initialized")
	return metrics, provider.Shutdown, nil
}

func newMetrics(meter metric.Meter) (*Metrics, error) {
	var err error
	m := &Metrics{}

	if m.startupSuccess, err = meter.Int64Counter("deviser.startup.success"); err != nil {
		return nil, err
	}
	if m.startupFailure, err = meter.Int64Counter("deviser.startup.failure"); err != nil {
		return nil, err
	}
	if m.dbConnectDuration, err = meter.Float64Histogram("deviser.db.connect.duration_seconds"); err != nil {
		return nil, err
	}
	if m.dbSchemaApplyDuration, err = meter.Float64Histogram("deviser.db.schema_apply.duration_seconds"); err != nil {
		return nil, err
	}
	if m.dbSchemaApplyFailure, err = meter.Int64Counter("deviser.db.schema_apply.failure"); err != nil {
		return nil, err
	}
	if m.partitionsEnsureDuration, err = meter.Float64Histogram("deviser.partitions.ensure.duration_seconds"); err != nil {
		return nil, err
	}
	if m.partitionsEnsureFailure, err = meter.Int64Counter("deviser.partitions.ensure.failure"); err != nil {
		return nil, err
	}
	if m.partitionsMaintainDur, err = meter.Float64Histogram("deviser.partitions.maintain.duration_seconds"); err != nil {
		return nil, err
	}
	if m.partitionsMaintainFail, err = meter.Int64Counter("deviser.partitions.maintain.failure"); err != nil {
		return nil, err
	}
	if m.partitionsDroppedExpired, err = meter.Int64Counter("deviser.partitions.dropped.expired"); err != nil {
		return nil, err
	}
	if m.partitionsDroppedQuota, err = meter.Int64Counter("deviser.partitions.dropped.quota"); err != nil {
		return nil, err
	}
	if m.partitionsBytesFreed, err = meter.Int64Counter("deviser.partitions.bytes_freed"); err != nil {
		return nil, err
	}
	if m.resourcesPurgeDuration, err = meter.Float64Histogram("deviser.resources.purge.duration_seconds"); err != nil {
		return nil, err
	}
	if m.resourcesPurgeRows, err = meter.Int64Counter("deviser.resources.purge.rows"); err != nil {
		return nil, err
	}
	if m.resourcesPurgeFailure, err = meter.Int64Counter("deviser.resources.purge.failure"); err != nil {
		return nil, err
	}
	if m.loopIterationSuccess, err = meter.Int64Counter("deviser.loop.iteration.success"); err != nil {
		return nil, err
	}
	if m.loopIterationFailure, err = meter.Int64Counter("deviser.loop.iteration.failure"); err != nil {
		return nil, err
	}
	if m.partitionsEnsureDaysGauge, err = meter.Int64ObservableGauge("deviser.partitions.ensure.days"); err != nil {
		return nil, err
	}
	if m.partitionsTotalGauge, err = meter.Int64ObservableGauge("deviser.partitions.total_discovered"); err != nil {
		return nil, err
	}
	if m.partitionsTotalBytesGauge, err = meter.Int64ObservableGauge("deviser.partitions.total_bytes"); err != nil {
		return nil, err
	}

	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(m.partitionsEnsureDaysGauge, m.partitionsEnsureDays.Load())
		observer.ObserveInt64(m.partitionsTotalGauge, m.partitionsTotal.Load())
		observer.ObserveInt64(m.partitionsTotalBytesGauge, m.partitionsTotalBytes.Load())
		return nil
	}, m.partitionsEnsureDaysGauge, m.partitionsTotalGauge, m.partitionsTotalBytesGauge)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) IncStartupSuccess(ctx context.Context) {
	if m == nil {
		return
	}
	m.startupSuccess.Add(ctx, 1)
}

func (m *Metrics) IncStartupFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.startupFailure.Add(ctx, 1)
}

func (m *Metrics) RecordDBConnectDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.dbConnectDuration.Record(ctx, d.Seconds())
}

func (m *Metrics) RecordSchemaApplyDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.dbSchemaApplyDuration.Record(ctx, d.Seconds())
}

func (m *Metrics) IncSchemaApplyFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.dbSchemaApplyFailure.Add(ctx, 1)
}

func (m *Metrics) SetPartitionsEnsureDays(days int64) {
	if m == nil {
		return
	}
	m.partitionsEnsureDays.Store(days)
}

func (m *Metrics) RecordPartitionsEnsureDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.partitionsEnsureDuration.Record(ctx, d.Seconds())
}

func (m *Metrics) IncPartitionsEnsureFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.partitionsEnsureFailure.Add(ctx, 1)
}

func (m *Metrics) RecordPartitionsMaintainDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.partitionsMaintainDur.Record(ctx, d.Seconds())
}

func (m *Metrics) IncPartitionsMaintainFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.partitionsMaintainFail.Add(ctx, 1)
}

func (m *Metrics) AddPartitionsDroppedExpired(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.partitionsDroppedExpired.Add(ctx, n)
}

func (m *Metrics) AddPartitionsDroppedQuota(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.partitionsDroppedQuota.Add(ctx, n)
}

func (m *Metrics) AddPartitionsBytesFreed(ctx context.Context, bytes int64) {
	if m == nil || bytes <= 0 {
		return
	}
	m.partitionsBytesFreed.Add(ctx, bytes)
}

func (m *Metrics) SetPartitionsTotalDiscovered(n int64) {
	if m == nil {
		return
	}
	m.partitionsTotal.Store(n)
}

func (m *Metrics) SetPartitionsTotalBytes(bytes int64) {
	if m == nil {
		return
	}
	m.partitionsTotalBytes.Store(bytes)
}

func (m *Metrics) RecordResourcesPurgeDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.resourcesPurgeDuration.Record(ctx, d.Seconds())
}

func (m *Metrics) AddResourcesPurgeRows(ctx context.Context, rows int64) {
	if m == nil || rows <= 0 {
		return
	}
	m.resourcesPurgeRows.Add(ctx, rows)
}

func (m *Metrics) IncResourcesPurgeFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.resourcesPurgeFailure.Add(ctx, 1)
}

func (m *Metrics) IncLoopIterationSuccess(ctx context.Context) {
	if m == nil {
		return
	}
	m.loopIterationSuccess.Add(ctx, 1)
}

func (m *Metrics) IncLoopIterationFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.loopIterationFailure.Add(ctx, 1)
}
