package pg

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/krateoplatformops/deviser/internal/telemetry"
)

type CreateDailyPartitionsOptions struct {
	Pool    *pgxpool.Pool
	Log     *slog.Logger
	Tpl     *template.Template
	Days    int
	Metrics *telemetry.Metrics
}

func CreateDailyPartitions(ctx context.Context, opts *CreateDailyPartitionsOptions) (retErr error) {
	started := time.Now()
	opts.Metrics.SetPartitionsEnsureDays(int64(opts.Days))
	defer func() {
		opts.Metrics.RecordPartitionsEnsureDuration(ctx, time.Since(started))
		if retErr != nil {
			opts.Metrics.IncPartitionsEnsureFailure(ctx)
		}
	}()

	for i := 0; i < opts.Days; i++ {
		date := time.Now().AddDate(0, 0, i).UTC()
		partName := fmt.Sprintf("k8s_events_%04d_%02d_%02d", date.Year(), date.Month(), date.Day())
		startDate := date.Format("2006-01-02")
		endDate := date.AddDate(0, 0, 1).Format("2006-01-02")

		var sb strings.Builder
		err := opts.Tpl.Execute(&sb, map[string]string{
			"PartitionName": partName,
			"StartDate":     startDate,
			"EndDate":       endDate,
		})
		if err != nil {
			opts.Log.Error("failed to execute partition template",
				slog.Any("err", err))
			return err
		}

		if _, err := opts.Pool.Exec(ctx, sb.String()); err != nil {
			opts.Log.Error("failed to create partition",
				slog.String("partition", partName),
				slog.Any("err", err))
			return err
		}

		opts.Log.Debug("Partition ensured",
			slog.String("partition", partName))
	}

	return nil
}

func MustLoadPartitionTemplate(fs *embed.FS, log *slog.Logger) *template.Template {
	filename := "assets/partition.tpl.sql"
	content, err := fs.ReadFile(filename)
	if err != nil {
		log.Error("cannot read embedded file",
			slog.String("filename", filename),
			slog.Any("err", err))
		os.Exit(1)

		return nil
	}

	tpl, err := template.New("partition").Parse(string(content))
	if err != nil {
		log.Error("cannot parse template file",
			slog.String("filename", filename),
			slog.Any("err", err))
		os.Exit(1)
	}

	return tpl
}
