package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
)

func parseDBParams(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}

	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}

	params := make(map[string]string, len(values))
	for k, v := range values {
		if len(v) > 0 {
			params[k] = v[0] // PostgreSQL does not support multiple values
		}
	}

	return params, nil
}

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
)

func envSize(key string, defaultValue uint64) uint64 {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}

	size, err := parseSize(val)
	if err != nil {
		return defaultValue
	}

	return size

}

func parseSize(s string) (uint64, error) {
	var multiplier uint64 = 1

	switch {
	case strings.HasSuffix(s, "Gi"):
		multiplier = GB
		s = strings.TrimSuffix(s, "Gi")
	case strings.HasSuffix(s, "Mi"):
		multiplier = MB
		s = strings.TrimSuffix(s, "Mi")
	case strings.HasSuffix(s, "Ki"):
		multiplier = KB
		s = strings.TrimSuffix(s, "Ki")
	}

	val, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}

	return val * multiplier, nil
}
