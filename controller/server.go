package controller

import (
	"io/fs"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const cssPath = "assets/app.css"
const jsPath = "assets/app.js"

type Server struct {
	router *gin.Engine
	files  fs.FS
}

func NewServer(files fs.FS) *Server {
	if files == nil {
		files = os.DirFS(".")
	}

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	server := &Server{
		router: router,
		files:  files,
	}
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{Skip: func(c *gin.Context) bool {
		return c.GetBool("private_subscription") || strings.HasSuffix(c.Request.URL.Path, "/sing-box")
	}}), gin.Recovery(), server.securityHeaders(), server.sameOriginProtection())
	server.registerRoutes()
	return server
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) RunTLSWithSamePortRedirect(addr, certFile, keyFile string) error {
	return ListenAndServeTLSWithSamePortRedirect(addr, certFile, keyFile, s.router)
}

func (s *Server) registerRoutes() {
	s.router.GET("/assets/app.css", s.serveCSS)
	s.router.GET("/assets/app.js", s.serveJS)

	s.router.GET("/", s.dashboard)
	s.router.GET("/login", s.loginPage)
	s.router.POST("/login", s.login)
	s.router.POST("/logout", s.logout)
	s.router.POST("/subscription-path", s.saveSubscriptionPath)
	s.router.POST("/nodes", s.saveNodes)
	s.router.POST("/singbox-settings", s.saveSingBoxSettings)
	s.router.POST("/route-rules", s.createRouteRule)
	s.router.POST("/route-rules/:id", s.updateRouteRule)
	s.router.POST("/route-rules/:id/delete", s.deleteRouteRule)

	api := s.router.Group("/api")
	api.POST("/login", s.apiLogin)
	api.GET("/session", s.requireAPIAuth(), s.apiSession)
	api.POST("/logout", s.requireAPIAuth(), s.apiLogout)
	api.GET("/config", s.requireAPIAuth(), s.apiConfig)
	api.POST("/change_uri", s.requireAPIAuth(), s.apiChangeURI)
	api.POST("/change_nodes", s.requireAPIAuth(), s.apiChangeNodes)

	s.router.NoRoute(s.subscription)
}

func (s *Server) serveCSS(c *gin.Context) {
	content, err := fs.ReadFile(s.files, cssPath)
	if err != nil {
		c.String(statusInternalServerError, "failed to load css")
		return
	}
	c.Data(statusOK, "text/css; charset=utf-8", content)
}

func (s *Server) serveJS(c *gin.Context) {
	content, err := fs.ReadFile(s.files, jsPath)
	if err != nil {
		c.String(statusInternalServerError, "failed to load javascript")
		return
	}
	c.Data(statusOK, "text/javascript; charset=utf-8", content)
}
