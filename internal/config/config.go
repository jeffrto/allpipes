package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const minPollInterval = 2 * time.Second

type Config struct {
	GitLabURL           string
	GitLabToken         string
	Group               string
	PollInterval        time.Duration
	IncludeSubgroups    bool
	PipelinesPerProject int
	Concurrency         int
	Mode                string
	Port                int
	Host                string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	env := &envReader{}
	cfg := &Config{
		GitLabURL:           env.str("GITLAB_URL", "https://gitlab.com"),
		GitLabToken:         env.str("GITLAB_TOKEN", ""),
		Group:               env.str("GITLAB_GROUP", ""),
		PollInterval:        env.duration("POLL_INTERVAL", 10*time.Second),
		IncludeSubgroups:    env.boolean("INCLUDE_SUBGROUPS", true),
		PipelinesPerProject: env.integer("PIPELINES_PER_PROJECT", 3),
		Concurrency:         env.integer("CONCURRENCY", 8),
		Mode:                env.str("MODE", "tui"),
		Port:                env.integer("PORT", 8080),
		Host:                env.str("HOST", "127.0.0.1"),
	}
	if err := errors.Join(env.errs...); err != nil {
		return nil, err
	}

	var token string
	var web, tui bool
	flag.StringVar(&cfg.Group, "group", cfg.Group, "GitLab group path, ID or URL")
	flag.StringVar(&token, "token", "", "GitLab access token with read_api scope (default $GITLAB_TOKEN)")
	flag.StringVar(&cfg.GitLabURL, "url", cfg.GitLabURL, "GitLab base URL")
	flag.DurationVar(&cfg.PollInterval, "interval", cfg.PollInterval, "polling interval")
	flag.BoolVar(&cfg.IncludeSubgroups, "subgroups", cfg.IncludeSubgroups, "include projects in subgroups")
	flag.IntVar(&cfg.PipelinesPerProject, "recent", cfg.PipelinesPerProject, "recent pipelines to track per project")
	flag.IntVar(&cfg.Concurrency, "concurrency", cfg.Concurrency, "maximum concurrent API requests")
	flag.BoolVar(&tui, "tui", false, "run the terminal UI (default)")
	flag.BoolVar(&web, "web", false, "run the web dashboard")
	flag.StringVar(&cfg.Host, "host", cfg.Host, "web dashboard listen address")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "web dashboard port")
	flag.Usage = usage
	flag.Parse()

	if token != "" {
		cfg.GitLabToken = token
	}
	switch {
	case web && tui:
		return nil, errors.New("choose either -web or -tui, not both")
	case web:
		cfg.Mode = "web"
	case tui:
		cfg.Mode = "tui"
	}

	cfg.GitLabURL = strings.TrimRight(cfg.GitLabURL, "/")
	if !strings.HasPrefix(cfg.GitLabURL, "http://") && !strings.HasPrefix(cfg.GitLabURL, "https://") {
		cfg.GitLabURL = "https://" + cfg.GitLabURL
	}
	cfg.Group = normalizeGroup(cfg.Group, cfg.GitLabURL)

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var errs []error
	if c.Mode != "tui" && c.Mode != "web" {
		errs = append(errs, fmt.Errorf("MODE must be \"tui\" or \"web\", got %q", c.Mode))
	}
	if c.PollInterval < minPollInterval {
		errs = append(errs, fmt.Errorf("poll interval must be at least %s, got %s", minPollInterval, c.PollInterval))
	}
	if c.PipelinesPerProject < 1 || c.PipelinesPerProject > 100 {
		errs = append(errs, fmt.Errorf("recent pipelines per project must be between 1 and 100, got %d", c.PipelinesPerProject))
	}
	if c.Concurrency < 1 {
		errs = append(errs, fmt.Errorf("concurrency must be at least 1, got %d", c.Concurrency))
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("port must be between 1 and 65535, got %d", c.Port))
	}
	return errors.Join(errs...)
}

func normalizeGroup(group, baseURL string) string {
	g := strings.TrimSpace(group)
	if u, err := url.Parse(g); err == nil && u.Host != "" {
		g = u.Path
		if base, err := url.Parse(baseURL); err == nil && base.Host == u.Host {
			g = strings.TrimPrefix(g, strings.TrimRight(base.Path, "/"))
		}
	}
	if i := strings.Index(g, "/-/"); i >= 0 {
		g = g[:i]
	}
	g = strings.Trim(g, "/")
	return strings.TrimPrefix(g, "groups/")
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprint(out, `allpipes watches the CI pipelines of every project in a GitLab group.

Usage:
  allpipes -group <path|id|url> [options]

Options:
`)
	flag.PrintDefaults()
	fmt.Fprint(out, `
Every option can also be set in the environment or a .env file:
  GITLAB_GROUP, GITLAB_TOKEN, GITLAB_URL, MODE (tui|web), POLL_INTERVAL,
  INCLUDE_SUBGROUPS, PIPELINES_PER_PROJECT, CONCURRENCY, HOST, PORT
`)
}

type envReader struct {
	errs []error
}

func (r *envReader) lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (r *envReader) str(key, def string) string {
	if v, ok := r.lookup(key); ok {
		return v
	}
	return def
}

func (r *envReader) integer(key string, def int) int {
	v, ok := r.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a whole number", key, v))
		return def
	}
	return n
}

func (r *envReader) boolean(key string, def bool) bool {
	v, ok := r.lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not true or false", key, v))
		return def
	}
	return b
}

func (r *envReader) duration(key string, def time.Duration) time.Duration {
	v, ok := r.lookup(key)
	if !ok {
		return def
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a duration like 10s or 1m", key, v))
		return def
	}
	return d
}
