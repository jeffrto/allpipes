package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"allpipes/internal/gitlab"

	"github.com/gin-gonic/gin"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	monitor *gitlab.Monitor
	engine  *gin.Engine
	host    string
	port    int
	srv     *http.Server
}

func NewServer(monitor *gitlab.Monitor, host string, port int) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{
		monitor: monitor,
		engine:  r,
		host:    host,
		port:    port,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.engine.GET("/", func(c *gin.Context) {
		indexData, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to load dashboard: %v", err)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
	})

	s.engine.GET("/api/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, s.monitor.GetSnapshot())
	})

	s.engine.POST("/api/refresh", func(c *gin.Context) {
		s.monitor.TriggerRefresh()
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "refresh triggered"})
	})

	s.engine.GET("/api/events", func(c *gin.Context) {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")

		initialSnap := s.monitor.GetSnapshot()
		if data, err := json.Marshal(initialSnap); err == nil {
			c.SSEvent("snapshot", string(data))
			c.Writer.Flush()
		}

		snapChan := s.monitor.Subscribe()
		defer s.monitor.Unsubscribe(snapChan)

		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		ctx := c.Request.Context()

		c.Stream(func(w io.Writer) bool {
			select {
			case <-ctx.Done():
				return false
			case <-heartbeat.C:
				c.SSEvent("ping", "")
				return true
			case snap, ok := <-snapChan:
				if !ok {
					return false
				}
				data, err := json.Marshal(snap)
				if err != nil {
					return true
				}
				c.SSEvent("snapshot", string(data))
				return true
			}
		})
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}
