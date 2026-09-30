package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"allpipes/internal/config"
	"allpipes/internal/gitlab"
	"allpipes/internal/tui"
	"allpipes/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "allpipes: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Group == "" {
		return errors.New("no group given. Pass -group <path> or set GITLAB_GROUP (see allpipes -h)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := gitlab.NewClient(cfg.GitLabURL, cfg.GitLabToken)

	fmt.Fprintf(os.Stderr, "Connecting to %s...\n", cfg.GitLabURL)
	group, err := client.GetGroup(ctx, cfg.Group)
	if err != nil {
		return explainGroupError(err, cfg)
	}

	monitor := gitlab.NewMonitor(client, gitlab.MonitorOptions{
		Group:               *group,
		IncludeSubgroups:    cfg.IncludeSubgroups,
		PollInterval:        cfg.PollInterval,
		PipelinesPerProject: cfg.PipelinesPerProject,
		Concurrency:         cfg.Concurrency,
	})
	go monitor.Run(ctx)

	if cfg.Mode == "web" {
		return serveWeb(ctx, monitor, cfg, group)
	}
	return tui.Run(ctx, monitor)
}

func serveWeb(ctx context.Context, monitor *gitlab.Monitor, cfg *config.Config, group *gitlab.Group) error {
	server := web.NewServer(monitor, cfg.Host, cfg.Port)
	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()

	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	fmt.Printf("Watching %s. Dashboard at http://%s:%d\n", group.FullPath, host, cfg.Port)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("web server: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func explainGroupError(err error, cfg *config.Config) error {
	switch gitlab.StatusCode(err) {
	case 0:
		var netErr *url.Error
		if errors.As(err, &netErr) {
			return fmt.Errorf("could not reach %s: %v", cfg.GitLabURL, netErr.Err)
		}
		return fmt.Errorf("%v\nIs %s a GitLab instance? Set it with -url or GITLAB_URL", err, cfg.GitLabURL)
	case http.StatusUnauthorized:
		return fmt.Errorf("%v\nCreate a personal or group access token with the read_api scope and pass it with -token or GITLAB_TOKEN", err)
	case http.StatusForbidden:
		return fmt.Errorf("%v\nThe token cannot read group %q. It needs the read_api scope", err, cfg.Group)
	case http.StatusNotFound:
		if cfg.GitLabToken == "" {
			return fmt.Errorf("group %q was not found on %s. Private groups need a token: pass -token or set GITLAB_TOKEN", cfg.Group, cfg.GitLabURL)
		}
		return fmt.Errorf("group %q was not found on %s, or the token's user is not a member of it", cfg.Group, cfg.GitLabURL)
	}
	return err
}
