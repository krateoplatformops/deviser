package config

import (
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	fsutil "github.com/krateoplatformops/deviser/internal/util/fs"
	logutil "github.com/krateoplatformops/deviser/internal/util/log"
	pgutil "github.com/krateoplatformops/deviser/internal/util/pg"
	"github.com/krateoplatformops/plumbing/env"
)

const (
	serviceName           = "deviser"
	defaultPartitionDays  = 7
	defaultDbReadyTimeout = 3 * time.Minute
	defaultDebug          = false
)

//go:embed assets/*.sql
var assetsFS embed.FS

type Config struct {
	Port            int
	Debug           bool
	DbURL           string
	DbReadyTimeout  time.Duration
	DbPartitionDays int
	Log             *slog.Logger
}

func Setup() *Config {
	cfg := &Config{}

	cfgPort := flag.Int("port",
		env.ServicePort("PORT", 8081),
		"port to listen on",
	)

	cfgDebug := flag.Bool("debug",
		env.Bool("DEBUG", defaultDebug),
		"enable or disable debug logs",
	)

	cfgDbUser := flag.String("db-user",
		env.String("DB_USER", ""),
		"database connection username",
	)

	cfgDbPass := flag.String("db-pass",
		env.String("DB_PASS", ""),
		"database connection password",
	)

	cfgDbName := flag.String("db-name",
		env.String("DB_NAME", ""),
		"database name",
	)

	cfgDbHost := flag.String("db-host",
		env.String("DB_HOST", "localhost"),
		"database host",
	)
	cfgDbPort := flag.Int("db-port",
		env.Int("DB_PORT", 5432),
		"database port",
	)

	cfgDbParams := flag.String("db-params",
		env.String("DB_PARAMS", ""),
		"extra database query params (es: sslmode=disable&connect_timeout=5)",
	)

	cfgDbReadyTimeout := flag.Duration("db-ready-timeout",
		env.Duration("DB_READY_TIMEOUT", defaultDbReadyTimeout),
		"maximum time to wait for PostgreSQL to become ready",
	)

	cfgDbPartitions := flag.Int("db-partitions-days",
		env.Int("DB_PARTITIONS_DAYS", defaultPartitionDays),
		"partitions for the next N days",
	)

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Copy values
	cfg.Port = *cfgPort
	cfg.Debug = *cfgDebug
	cfg.DbReadyTimeout = *cfgDbReadyTimeout
	cfg.DbPartitionDays = *cfgDbPartitions

	cfg.Log = logutil.New(serviceName, cfg.Debug)

	params, err := parseDBParams(*cfgDbParams)
	if err != nil {
		cfg.Log.Error("invalid DB_PARAMS", "error", err)
		os.Exit(1)
	}

	cfg.DbURL, err = pgutil.ConnectionURL(
		*cfgDbUser,
		*cfgDbPass,
		*cfgDbHost,
		*cfgDbPort,
		*cfgDbName,
		params)
	if err != nil {
		cfg.Log.Error("unable to build DB_URL", slog.Any("err", err))
		os.Exit(1)
	}

	if cfg.Debug {
		cfg.Log.Debug("database connection URL", slog.String("cfg.DbURL", cfg.DbURL))
	}

	return cfg
}

func (c *Config) MustLoadSQL(filename string) string {
	res, err := c.LoadSQL(filename)
	if err != nil {
		os.Exit(1)
	}

	return res
}

func (c *Config) LoadSQL(filename string) (string, error) {
	if !strings.HasPrefix("assets/", filename) {
		filename = path.Join("assets", filename)
	}

	content, err := fsutil.ReadFile(assetsFS, filename)
	if err != nil {
		c.Log.Error("cannot read embedded file",
			slog.String("filename", filename),
			slog.Any("err", err))
		return "", err

	}

	return string(content), nil
}

func (c *Config) MustLoadSQLTemplate(filename, tplID string) *template.Template {
	res, err := c.LoadSQLTemplate(filename, tplID)
	if err != nil {
		os.Exit(1)
	}
	return res
}

func (c *Config) LoadSQLTemplate(filename, tplID string) (*template.Template, error) {
	src, err := c.LoadSQL(filename)
	if err != nil {
		return nil, err
	}

	tpl, err := template.New(tplID).Parse(src)
	if err != nil {
		c.Log.Error("cannot parse template file",
			slog.String("filename", filename),
			slog.Any("err", err))
		return nil, err
	}

	return tpl, nil
}
