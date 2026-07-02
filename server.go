package main

import (
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.local/gosub/lib"
	"git.local/gosub/views"
	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

//go:embed assets/app.css
var assetsFS embed.FS

type Server struct {
	router *gin.Engine
}

func NewServer() *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	server := &Server{router: router}
	server.registerRoutes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) registerRoutes() {
	s.router.GET("/assets/app.css", serveCSS)

	s.router.GET("/", s.dashboard)
	s.router.GET("/login", s.loginPage)
	s.router.POST("/login", s.login)
	s.router.POST("/logout", s.logout)
	s.router.POST("/subscription-path", s.saveSubscriptionPath)
	s.router.POST("/nodes", s.saveNodes)

	api := s.router.Group("/api")
	api.POST("/login", s.apiLogin)
	api.GET("/session", s.requireAPIAuth(), s.apiSession)
	api.POST("/logout", s.requireAPIAuth(), s.apiLogout)
	api.GET("/config", s.requireAPIAuth(), s.apiConfig)
	api.POST("/change_uri", s.requireAPIAuth(), s.apiChangeURI)
	api.POST("/change_nodes", s.requireAPIAuth(), s.apiChangeNodes)

	s.router.NoRoute(s.subscription)
}

func serveCSS(c *gin.Context) {
	content, err := assetsFS.ReadFile("assets/app.css")
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load css")
		return
	}
	c.Data(http.StatusOK, "text/css; charset=utf-8", content)
}

func (s *Server) dashboard(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}
	s.renderDashboard(c, http.StatusOK, flashMessage(c.Query("msg")))
}

func (s *Server) loginPage(c *gin.Context) {
	if _, ok := sessionFromRequest(c.Request); ok {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	render(c, http.StatusOK, views.LoginPage(views.LoginData{Message: flashMessage(c.Query("msg"))}))
}

func (s *Server) login(c *gin.Context) {
	if !lib.Authenticate(c.PostForm("username"), c.PostForm("password")) {
		c.Redirect(http.StatusSeeOther, "/login?msg=login_failed")
		return
	}
	if err := createSessionCookie(c.Writer); err != nil {
		c.String(http.StatusInternalServerError, "failed to create session")
		return
	}
	c.Redirect(http.StatusSeeOther, "/?msg=login_ok")
}

func (s *Server) logout(c *gin.Context) {
	clearSessionCookie(c.Writer, c.Request)
	c.Redirect(http.StatusSeeOther, "/login?msg=logged_out")
}

func (s *Server) saveSubscriptionPath(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	nextPath := strings.TrimSpace(c.PostForm("path"))
	if c.PostForm("action") == "random" {
		random, err := lib.RandomString(8)
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to generate path")
			return
		}
		nextPath = "/" + random
	}
	if err := lib.UpdateSubscriptionPath(nextPath); err != nil {
		s.renderDashboard(c, http.StatusBadRequest, "订阅路径必须以 / 开头，且不能包含空白字符")
		return
	}
	s.renderDashboard(c, http.StatusOK, "订阅路径已保存")
}

func (s *Server) saveNodes(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	nodesText := strings.ReplaceAll(c.PostForm("nodes"), "\r\n", "\n")
	if err := lib.UpdateNodes(strings.Split(nodesText, "\n")); err != nil {
		c.String(http.StatusInternalServerError, "failed to save nodes")
		return
	}
	s.renderDashboard(c, http.StatusOK, "节点列表已保存")
}

func (s *Server) subscription(c *gin.Context) {
	settings, err := lib.GetSettings()
	if err != nil {
		c.String(http.StatusInternalServerError, "config error")
		return
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		c.String(http.StatusInternalServerError, "subscription path error")
		return
	}
	if c.Request.URL.Path != path {
		c.String(http.StatusNotFound, "404 page not found")
		return
	}
	if c.Request.Method != http.MethodGet {
		c.String(http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := lib.GetB64FromLib()
	if err != nil {
		log.Printf("get subscription data: %v", err)
		body = ""
	}

	ip, port, _ := net.SplitHostPort(c.Request.RemoteAddr)
	log.Printf(
		`time=%s src=%s:%s method=%s path=%q ua=%q referer=%q lang=%q`,
		time.Now().Format("2006-01-02 15:04:05"), ip, port,
		c.Request.Method,
		c.Request.URL.RequestURI(),
		c.GetHeader("User-Agent"),
		c.GetHeader("Referer"),
		c.GetHeader("Accept-Language"),
	)

	if settings.TLS != nil && settings.TLS.Enabled {
		c.Header("Strict-Transport-Security", "max-age=31536000")
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func (s *Server) apiLogin(c *gin.Context) {
	if !lib.Authenticate(c.PostForm("username"), c.PostForm("password")) {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	if err := createSessionCookie(c.Writer); err != nil {
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	c.Status(http.StatusOK)
}

func (s *Server) apiSession(c *gin.Context) {
	sessionData := c.MustGet("session").(lib.SessionData)
	c.JSON(http.StatusOK, gin.H{
		"authenticated": true,
		"user_id":       sessionData.UserID,
	})
}

func (s *Server) apiLogout(c *gin.Context) {
	clearSessionCookie(c.Writer, c.Request)
	c.Status(http.StatusOK)
}

func (s *Server) apiConfig(c *gin.Context) {
	cfg, err := lib.GetConfigSnapshot()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to load config")
		return
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to load subscription path")
		return
	}
	subURL, err := lib.GetSubscriptionURL()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to build subscription URL")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"settings":          cfg.Settings,
		"nodes":             cfg.Nodes,
		"subscription_path": path,
		"subscription_url":  subURL,
	})
}

func (s *Server) apiChangeURI(c *gin.Context) {
	newURI := strings.TrimSpace(c.PostForm("uri"))
	if err := lib.UpdateSubscriptionPath(newURI); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func (s *Server) apiChangeNodes(c *gin.Context) {
	if err := lib.UpdateNodes(c.PostFormArray("nodes")); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func (s *Server) requireAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionData, ok := sessionFromRequest(c.Request)
		if !ok {
			c.String(http.StatusUnauthorized, "Unauthorized")
			c.Abort()
			return
		}
		c.Set("session", sessionData)
		c.Next()
	}
}

func (s *Server) requireWebAuth(c *gin.Context) bool {
	if _, ok := sessionFromRequest(c.Request); ok {
		return true
	}
	c.Redirect(http.StatusSeeOther, "/login")
	return false
}

func (s *Server) renderDashboard(c *gin.Context, status int, message string) {
	cfg, err := lib.GetConfigSnapshot()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load config")
		return
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load subscription path")
		return
	}
	subURL, err := lib.GetSubscriptionURL()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to build subscription URL")
		return
	}

	render(c, status, views.DashboardPage(views.DashboardData{
		Settings:         cfg.Settings,
		SubscriptionPath: path,
		SubscriptionURL:  subURL,
		NodesText:        strings.Join(cfg.Nodes, "\n"),
		NodeCount:        len(cfg.Nodes),
		Message:          message,
	}))
}

func sessionFromRequest(r *http.Request) (lib.SessionData, bool) {
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		return lib.SessionData{}, false
	}
	return lib.GetSession(cookie.Value)
}

func createSessionCookie(w http.ResponseWriter) error {
	settings, err := lib.GetSettings()
	if err != nil {
		return err
	}
	sid, err := lib.GenerateSessionID()
	if err != nil {
		return err
	}
	userID, err := lib.RandUserID()
	if err != nil {
		return err
	}

	expiration := time.Now().Add(2160 * time.Hour)
	if err := lib.SetSession(sid, lib.SessionData{UserID: userID, ExpiresAt: expiration}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sid,
		Expires:  expiration,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   settings.TLS != nil && settings.TLS.Enabled,
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil && cookie.Value != "" {
		lib.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("template error: %v", err))
	}
}

func flashMessage(code string) string {
	switch code {
	case "login_failed":
		return "用户名或密码错误"
	case "logged_out":
		return "已退出登录"
	case "login_ok":
		return "登录成功"
	default:
		return ""
	}
}

func startupURLs(settings *lib.Settings, path string) (string, string) {
	scheme := "http"
	if settings.TLS != nil && settings.TLS.Enabled {
		scheme = "https"
	}
	host := settings.Host
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := scheme + "://" + net.JoinHostPort(host, strconv.Itoa(settings.Port))
	return base + "/", base + path
}
