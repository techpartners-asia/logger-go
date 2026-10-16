package logger

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is a plain, serialisable description of a logger. It can be filled
// from environment variables with ConfigFromEnv, decoded from YAML/JSON by the
// caller, or built by hand, and then applied with WithConfig.
type Config struct {
	// Level: trace, debug, info, warn, error, fatal. Default info.
	Level string `json:"level" yaml:"level" env:"LOG_LEVEL"`
	// Format: json, console, text. Default json.
	Format string `json:"format" yaml:"format" env:"LOG_FORMAT"`
	// Output: "stderr", "stdout" or a file path. Default stderr.
	Output string `json:"output" yaml:"output" env:"LOG_OUTPUT"`
	// Service, Env and Version become fields on every record.
	Service string `json:"service" yaml:"service" env:"LOG_SERVICE"`
	Env     string `json:"env" yaml:"env" env:"LOG_ENV"`
	Version string `json:"version" yaml:"version" env:"LOG_VERSION"`
	// Source adds file:line to records.
	Source bool `json:"source" yaml:"source" env:"LOG_SOURCE"`
	// Color forces console color on/off; nil means auto-detect.
	Color *bool `json:"color,omitempty" yaml:"color,omitempty" env:"LOG_COLOR"`
	// UTC renders timestamps in UTC.
	UTC bool `json:"utc" yaml:"utc" env:"LOG_UTC"`
	// TimeFormat is a Go time layout.
	TimeFormat string `json:"time_format" yaml:"time_format" env:"LOG_TIME_FORMAT"`
	// ProcessInfo adds host and pid.
	ProcessInfo bool `json:"process_info" yaml:"process_info" env:"LOG_PROCESS_INFO"`
	// Redact enables redaction; RedactKeys adds keys to the default set.
	Redact     bool     `json:"redact" yaml:"redact" env:"LOG_REDACT"`
	RedactKeys []string `json:"redact_keys" yaml:"redact_keys" env:"LOG_REDACT_KEYS"`
	// SamplingInitial / SamplingThereafter enable sampling when > 0.
	SamplingInitial    int `json:"sampling_initial" yaml:"sampling_initial" env:"LOG_SAMPLING_INITIAL"`
	SamplingThereafter int `json:"sampling_thereafter" yaml:"sampling_thereafter" env:"LOG_SAMPLING_THEREAFTER"`
	// StackTraceLevel attaches stack traces at or above this level ("" = off).
	StackTraceLevel string `json:"stack_trace_level" yaml:"stack_trace_level" env:"LOG_STACK_TRACE_LEVEL"`
	// Keys selects a key naming scheme: "default", "elastic", "datadog".
	Keys string `json:"keys" yaml:"keys" env:"LOG_KEYS"`
	// Dir enables a rotating log file in this directory (see FileConfig).
	// Shorthand for File.Dir.
	Dir string `json:"dir" yaml:"dir" env:"LOG_DIR"`
	// File configures the rotating log file. Used when Dir or File.Dir is set.
	File FileConfig `json:"file" yaml:"file"`
}

// ConfigFromEnv reads LOG_* environment variables. It also falls back to the
// common SERVICE_NAME, APP_ENV / ENVIRONMENT and APP_VERSION variables for the
// service, env and version fields, and honours NO_COLOR.
func ConfigFromEnv() Config {
	return ConfigFromLookup(os.LookupEnv)
}

// ConfigFromLookup is ConfigFromEnv with a custom variable source, handy for
// tests or for .env parsers.
func ConfigFromLookup(lookup func(string) (string, bool)) Config {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := lookup(k); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	getBool := func(keys ...string) bool {
		b, _ := strconv.ParseBool(get(keys...))
		return b
	}
	getInt := func(keys ...string) int {
		n, _ := strconv.Atoi(get(keys...))
		return n
	}

	c := Config{
		Level:              get("LOG_LEVEL"),
		Format:             get("LOG_FORMAT"),
		Output:             get("LOG_OUTPUT"),
		Service:            get("LOG_SERVICE", "SERVICE_NAME", "APP_NAME"),
		Env:                get("LOG_ENV", "APP_ENV", "ENVIRONMENT", "ENV"),
		Version:            get("LOG_VERSION", "APP_VERSION", "SERVICE_VERSION"),
		Source:             getBool("LOG_SOURCE"),
		UTC:                getBool("LOG_UTC"),
		TimeFormat:         get("LOG_TIME_FORMAT"),
		ProcessInfo:        getBool("LOG_PROCESS_INFO"),
		Redact:             getBool("LOG_REDACT"),
		SamplingInitial:    getInt("LOG_SAMPLING_INITIAL"),
		SamplingThereafter: getInt("LOG_SAMPLING_THEREAFTER"),
		StackTraceLevel:    get("LOG_STACK_TRACE_LEVEL"),
		Keys:               get("LOG_KEYS"),
		Dir:                get("LOG_DIR"),
		File: FileConfig{
			Filename:   get("LOG_FILE"),
			MaxSizeMB:  getInt("LOG_FILE_MAX_SIZE_MB"),
			MaxBackups: getInt("LOG_FILE_MAX_BACKUPS"),
			MaxAgeDays: getInt("LOG_FILE_MAX_AGE_DAYS"),
			Daily:      getBool("LOG_FILE_DAILY"),
			Compress:   getBool("LOG_FILE_COMPRESS"),
			Tee:        getBool("LOG_FILE_TEE"),
		},
	}
	if v := get("LOG_REDACT_KEYS"); v != "" {
		c.Redact = true
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				c.RedactKeys = append(c.RedactKeys, k)
			}
		}
	}
	if v, ok := lookup("LOG_COLOR"); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			c.Color = &b
		}
	} else if _, ok := lookup("NO_COLOR"); ok {
		f := false
		c.Color = &f
	}
	return c
}

// Options converts the Config into a list of Option values.
func (c Config) Options() []Option {
	var opts []Option
	if c.Level != "" {
		opts = append(opts, WithLevelString(c.Level))
	}
	if c.Format != "" {
		opts = append(opts, WithFormat(ParseFormat(c.Format)))
	}
	switch strings.ToLower(c.Output) {
	case "", "stderr":
	case "stdout":
		opts = append(opts, WithOutput(os.Stdout))
	default:
		opts = append(opts, withFileOutput(c.Output))
	}
	if c.Service != "" {
		opts = append(opts, WithService(c.Service))
	}
	if c.Env != "" {
		opts = append(opts, WithEnv(c.Env))
	}
	if c.Version != "" {
		opts = append(opts, WithVersion(c.Version))
	}
	if c.Source {
		opts = append(opts, WithSource(true))
	}
	if c.Color != nil {
		opts = append(opts, WithColor(*c.Color))
	}
	if c.UTC {
		opts = append(opts, WithUTC())
	}
	if c.TimeFormat != "" {
		opts = append(opts, WithTimeFormat(c.TimeFormat))
	}
	if c.ProcessInfo {
		opts = append(opts, WithProcessInfo())
	}
	if c.Redact || len(c.RedactKeys) > 0 {
		opts = append(opts, WithRedaction(c.RedactKeys...))
	}
	if c.SamplingInitial > 0 || c.SamplingThereafter > 0 {
		opts = append(opts, WithSampling(Sampling{
			Initial:    c.SamplingInitial,
			Thereafter: c.SamplingThereafter,
			Tick:       time.Second,
		}))
	}
	if c.StackTraceLevel != "" {
		if l, err := ParseLevel(c.StackTraceLevel); err == nil {
			opts = append(opts, WithStackTrace(l))
		}
	}
	if c.Dir != "" || c.File.Dir != "" {
		f := c.File
		if f.Dir == "" {
			f.Dir = c.Dir
		}
		opts = append(opts, WithFile(f))
	}
	switch strings.ToLower(c.Keys) {
	case "elastic", "ecs":
		opts = append(opts, WithKeyNames(ElasticKeyNames))
	case "datadog", "dd":
		opts = append(opts, WithKeyNames(DatadogKeyNames))
	}
	return opts
}

// withFileOutput opens (or creates) a file for appending. Errors are deferred
// to New so callers get them from the usual error return.
func withFileOutput(path string) Option {
	return func(o *options) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			o.output = &errWriter{err: err}
			return
		}
		o.output = f
		o.closer = f
	}
}

// errWriter carries an open error until New inspects it.
type errWriter struct{ err error }

func (e *errWriter) Write([]byte) (int, error) { return 0, e.err }
