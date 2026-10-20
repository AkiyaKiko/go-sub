package controller

import (
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

const cssPath = "assets/app.css"
const jsPath = "assets/app.js"

type Server struct {
	router         *gin.Engine
	files          fs.FS
	trustedProxies []*net.IPNet
	loginLimiter   loginLimiter
	writeSlots     chan struct{}
}

func NewServer(files fs.FS) *Server {
	if files == nil {
		files = os.DirFS(".")
	}

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	server := &Server{
		router:     router,
		files:      files,
		writeSlots: make(chan struct{}, 4),
	}
	proxies := []string{"127.0.0.1/32", "::1/128"}
	if settings, err := lib.GetSettings(); err == nil && settings.TrustedProxies != nil {
		proxies = settings.TrustedProxies
	}
	if err := router.SetTrustedProxies(proxies); err != nil {
		log.Print("invalid proxy configuration; forwarded headers disabled")
		_ = router.SetTrustedProxies(nil)
		proxies = nil
	}
	for _, value := range proxies {
		ip := net.ParseIP(value)
		if ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
				ip = ip.To4()
			}
			server.trustedProxies = append(server.trustedProxies, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		} else if _, network, err := net.ParseCIDR(value); err == nil {
			server.trustedProxies = append(server.trustedProxies, network)
		}
	}
	router.Use(gin.LoggerWithFormatter(func(params gin.LogFormatterParams) string {
		route := "[private-or-unmatched]"
		// Only registered route patterns may enter logs; public tokens and queries never do.
		if value, ok := params.Keys["log_route"].(string); ok {
			route = value
		}
		return fmt.Sprintf("[GIN] %s | %d | %s | %s | %s %q\n", params.TimeStamp.Format(time.RFC3339), params.StatusCode, params.Latency, params.ClientIP, params.Method, route)
	}), func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				log.Print("request handler panicked; request details redacted")
				c.AbortWithStatus(statusInternalServerError)
			}
		}()
		c.Next()
	}, server.securityHeaders(), server.sameOriginProtection(), server.requestLimits())
	server.registerRoutes()
	return server
}

func (s *Server) Run(addr string) error {
	return newHTTPServer(addr, s.router).ListenAndServe()
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 * 1024,
	}
}

func (s *Server) RunTLSWithSamePortRedirect(addr, certFile, keyFile string) error {
	return ListenAndServeTLSWithSamePortRedirect(addr, certFile, keyFile, s.router)
}

func (s *Server) registerRoutes() {
	s.router.GET("/assets/app.css", s.serveCSS)
	s.router.GET("/assets/app.js", s.serveJS)

	s.router.GET("/", s.dashboard)
	s.router.GET("/login", s.loginPage)
	s.router.POST("/login", s.loginProtection(), s.login)
	s.router.POST("/logout", s.logout)
	s.router.POST("/subscription-path", s.saveSubscriptionPath)
	s.router.POST("/nodes", s.saveNodes)
	s.router.POST("/singbox-settings", s.saveSingBoxSettings)
	s.router.POST("/singbox-template", s.saveSingBoxTemplate)
	s.router.POST("/route-rules", s.createRouteRule)
	s.router.POST("/route-rules/:id", s.updateRouteRule)
	s.router.POST("/route-rules/:id/delete", s.deleteRouteRule)

	api := s.router.Group("/api")
	api.POST("/login", s.loginProtection(), s.apiLogin)
	api.GET("/session", s.requireAPIAuth(), s.apiSession)
	api.POST("/logout", s.requireAPIAuth(), s.apiLogout)
	api.GET("/config", s.requireAPIAuth(), s.apiConfig)
	api.GET("/singbox-template", s.requireAPIAuth(), s.apiSingBoxTemplate)
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
