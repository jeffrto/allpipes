package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

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

	cfg := &Config{
		GitLabURL:           getEnv("GITLAB_URL", "https://gitlab.com"),
		GitLabToken:         getEnv("GITLAB_TOKEN", ""),
		Group:               getEnv("GITLAB_GROUP", ""),
		PollInterval:        getEnvDuration("POLL_INTERVAL", 10*time.Second),
		IncludeSubgroups:    getEnvBool("INCLUDE_SUBGROUPS", true),
		PipelinesPerProject: getEnvInt("PIPELINES_PER_PROJECT", 3),
		Concurrency:         getEnvInt("CONCURRENCY", 10),
		Mode:                getEnv("MODE", "tui"),
		Port:                getEnvInt("PORT", 8080),
		Host:                getEnv("HOST", "0.0.0.0"),
	}

	var (
		webFlag          = flag.Bool("web", false, "Start Web Dashboard mode (HTTP server)")
		tuiFlag          = flag.Bool("tui", false, "Start Terminal UI mode")
		groupFlag        = flag.String("group", "", "GitLab Group path or ID (e.g. 'gitlab-org' or 'team/project')")
		tokenFlag        = flag.String("token", "", "GitLab Personal/Group Access Token")
		urlFlag          = flag.String("url", "", "GitLab base URL (default: https://gitlab.com)")
		intervalFlag     = flag.Duration("interval", 0, "Polling interval (e.g. 5s, 10s, 30s)")
		portFlag         = flag.Int("port", 0, "Port for Web Dashboard (default: 8080)")
		hostFlag         = flag.String("host", "", "Host for Web Dashboard (default: 0.0.0.0)")
		subgroupsFlag    = flag.Bool("subgroups", true, "Include projects in subgroups")
		pipelinesNumFlag = flag.Int("recent", 0, "Number of recent pipelines to fetch per project (default: 3)")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: allpipes [options]\n\n")
		fmt.Fprintf(os.Stderr, "Real-time GitLab pipeline monitor across all projects in a group.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  GITLAB_GROUP           GitLab Group path or ID\n")
		fmt.Fprintf(os.Stderr, "  GITLAB_TOKEN           GitLab Personal Access Token with 'read_api'\n")
		fmt.Fprintf(os.Stderr, "  GITLAB_URL             GitLab base URL (default: https://gitlab.com)\n")
		fmt.Fprintf(os.Stderr, "  POLL_INTERVAL          Polling interval (e.g. 10s)\n")
		fmt.Fprintf(os.Stderr, "  PORT                   Web server port (default: 8080)\n")
		fmt.Fprintf(os.Stderr, "  MODE                   'tui' or 'web' (default: tui)\n")
	}

	flag.Parse()

	if *webFlag {
		cfg.Mode = "web"
	} else if *tuiFlag {
		cfg.Mode = "tui"
	}

	if *groupFlag != "" {
		cfg.Group = *groupFlag
	}
	if *tokenFlag != "" {
		cfg.GitLabToken = *tokenFlag
	}
	if *urlFlag != "" {
		cfg.GitLabURL = *urlFlag
	}
	if *intervalFlag > 0 {
		cfg.PollInterval = *intervalFlag
	}
	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	if *hostFlag != "" {
		cfg.Host = *hostFlag
	}
	if flag.Lookup("subgroups") != nil {
		cfg.IncludeSubgroups = *subgroupsFlag
	}
	if *pipelinesNumFlag > 0 {
		cfg.PipelinesPerProject = *pipelinesNumFlag
	}

	cfg.GitLabURL = strings.TrimRight(cfg.GitLabURL, "/")
	if !strings.HasPrefix(cfg.GitLabURL, "http://") && !strings.HasPrefix(cfg.GitLabURL, "https://") {
		cfg.GitLabURL = "https://" + cfg.GitLabURL
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
