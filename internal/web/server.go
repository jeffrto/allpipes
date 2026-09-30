package web

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"allpipes/internal/gitlab"

	"github.com/gin-gonic/gin"
)

//go:embed static/index.html
var staticFS embed.FS

type Server struct {
	monitor *gitlab.Monitor
	engine  *gin.Engine
	srv     *http.Server
	closing chan struct{}
}

func NewServer(monitor *gitlab.Monitor, host string, port int) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{monitor: monitor, engine: r, closing: make(chan struct{})}
	s.srv = &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	index, _ := staticFS.ReadFile("static/index.html")

	s.engine.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})

	s.engine.GET("/api/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, s.monitor.Snapshot())
	})

	s.engine.POST("/api/refresh", func(c *gin.Context) {
		s.monitor.TriggerRefresh()
		c.Status(http.StatusAccepted)
	})

	s.engine.GET("/api/events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")

		// Subscribe before taking the first snapshot so no update slips between them.
		updates := s.monitor.Subscribe()
		defer s.monitor.Unsubscribe(updates)

		send := func(snap gitlab.MonitorSnapshot) {
			if data, err := json.Marshal(snap); err == nil {
				c.SSEvent("snapshot", string(data))
			}
		}
		send(s.monitor.Snapshot())
		c.Writer.Flush()

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()

		c.Stream(func(io.Writer) bool {
			select {
			case <-c.Request.Context().Done():
				return false
			case <-s.closing:
				return false
			case <-heartbeat.C:
				c.SSEvent("ping", "")
				return true
			case snap, ok := <-updates:
				if ok {
					send(snap)
				}
				return ok
			}
		})
	})
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.closing)
	return s.srv.Shutdown(ctx)
}
