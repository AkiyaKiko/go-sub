package controller

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

const (
	statusOK                   = 200
	statusSeeOther             = 303
	statusBadRequest           = 400
	statusUnauthorized         = 401
	statusForbidden            = 403
	statusNotFound             = 404
	statusMethodNotAllowed     = 405
	statusRequestTooLarge      = 413
	statusUnsupportedMediaType = 415
	statusTooManyRequests      = 429
	statusInternalServerError  = 500
	statusServiceUnavailable   = 503
)

const (
	sessionCookieName = "session_id"
	sessionMaxAge     = 12 * 60 * 60
	sessionTTL        = 12 * time.Hour
	templatePattern   = "template/*.html"
)

type LoginData struct {
	Message string
}

type DashboardData struct {
	Settings          *lib.Settings        `json:"-"`
	ListenAddress     string               `json:"listen_address"`
	TLSStatus         string               `json:"tls_status"`
	SubscriptionPath  string               `json:"subscription_path"`
	SubscriptionURL   string               `json:"subscription_url"`
	SingBoxPath       string               `json:"singbox_path"`
	SingBoxURL        string               `json:"singbox_url"`
	SingBoxReport     lib.ConversionReport `json:"singbox_report"`
	SingBoxStatusText string               `json:"singbox_status_text"`
	SingBoxMode       string               `json:"singbox_mode"`
	NodesText         string               `json:"nodes_text"`
	NodeCount         int                  `json:"node_count"`
	RouteRules        []RouteRule          `json:"route_rules"`
	RouteRulesData    string               `json:"-"`
	DefaultRouteJSON  string               `json:"default_route_json"`
	Message           string               `json:"message"`
}

type RouteRule struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (s *Server) sameOriginProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		if !s.sameOriginRequest(c) {
			c.String(statusForbidden, "Forbidden")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if c.FullPath() != "" {
			c.Set("log_route", c.FullPath())
		}
		if !strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			headers.Set("Cache-Control", "no-store")
		}
		if s.requestScheme(c) == "https" {
			headers.Set("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

func isUnsafeMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func (s *Server) sameOriginRequest(c *gin.Context) bool {
	fetchSite := c.GetHeader("Sec-Fetch-Site")
	if fetchSite == "cross-site" {
		return false
	}
	origin := c.GetHeader("Origin")
	// no-referrer form navigations can hide Origin even for same-origin requests.
	if fetchSite == "same-origin" && (origin == "" || origin == "null") {
		return true
	}
	if origin != "" {
		return originMatchesHost(origin, s.requestScheme(c), c.Request.Host, true)
	}

	referer := c.GetHeader("Referer")
	if referer != "" {
		return originMatchesHost(referer, s.requestScheme(c), c.Request.Host, false)
	}

	return fetchSite == ""
}

func originMatchesHost(rawURL, scheme, host string, origin bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Scheme != scheme || (scheme != "http" && scheme != "https") {
		return false
	}
	if origin && (parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "") {
		return false
	}
	expected, err := url.Parse(scheme + "://" + host)
	if err != nil || expected.User != nil || expected.Host != host {
		return false
	}
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(parsed.Hostname(), expected.Hostname()) && port(parsed) == port(expected)
}

func (s *Server) requestScheme(c *gin.Context) string {
	if c.Request.TLS != nil {
		return "https"
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil {
		ip := net.ParseIP(host)
		for _, network := range s.trustedProxies {
			if network.Contains(ip) && c.GetHeader("X-Forwarded-Proto") == "https" {
				return "https"
			}
		}
	}
	return "http"
}

func (s *Server) requestLimits() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		if c.FullPath() == "" {
			c.Next()
			return
		}
		if c.Request.URL.Path != "/login" && c.Request.URL.Path != "/api/login" {
			if _, ok := sessionFromContext(c); !ok {
				if strings.HasPrefix(c.FullPath(), "/api/") {
					c.AbortWithStatus(statusUnauthorized)
				} else {
					c.Redirect(statusSeeOther, "/login")
					c.Abort()
				}
				return
			}
		}
		select {
		case s.writeSlots <- struct{}{}:
			defer func() { <-s.writeSlots }()
		default:
			c.Header("Retry-After", "5")
			c.AbortWithStatusJSON(statusTooManyRequests, gin.H{"message": "正在处理其他请求，请稍后重试"})
			return
		}
		limit := int64(2 * 1024 * 1024)
		switch c.Request.URL.Path {
		case "/login", "/api/login":
			limit = 8 * 1024
		case "/nodes", "/api/change_nodes":
			limit = 16 * 1024 * 1024
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(statusRequestTooLarge, gin.H{"message": "请求内容过大"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		if c.Request.ContentLength != 0 {
			contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || contentType != "application/x-www-form-urlencoded" {
				c.AbortWithStatusJSON(statusUnsupportedMediaType, gin.H{"message": "仅接受 URL 编码表单"})
				return
			}
		}
		if err := c.Request.ParseForm(); err != nil {
			status := statusBadRequest
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				status = statusRequestTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"message": "表单无效或超过大小限制"})
			return
		}
		for key, values := range c.Request.PostForm {
			if len(values) > 1 && !(c.Request.URL.Path == "/api/change_nodes" && key == "nodes") {
				c.AbortWithStatusJSON(statusBadRequest, gin.H{"message": "表单字段不能重复"})
				return
			}
		}
		c.Next()
	}
}

type loginWindow struct {
	started time.Time
	count   int
}
type loginLimiter struct {
	mu      sync.Mutex
	clients map[string]loginWindow
	total   loginWindow
}

func (limiter *loginLimiter) allow(client string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	const window = 5 * time.Minute
	if now.Sub(limiter.total.started) >= window {
		limiter.total = loginWindow{started: now}
	}
	if limiter.total.count >= 300 {
		return false
	}
	if limiter.clients == nil {
		limiter.clients = make(map[string]loginWindow)
	}
	entry := limiter.clients[client]
	if now.Sub(entry.started) >= window {
		entry = loginWindow{started: now}
	}
	if entry.count >= 10 {
		return false
	}
	if len(limiter.clients) >= 4096 {
		for key, value := range limiter.clients {
			if now.Sub(value.started) >= window {
				delete(limiter.clients, key)
			}
		}
		if _, exists := limiter.clients[client]; !exists && len(limiter.clients) >= 4096 {
			return false
		}
	}
	entry.count++
	limiter.total.count++
	limiter.clients[client] = entry
	return true
}

func (s *Server) loginProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.loginLimiter.allow(c.ClientIP(), time.Now()) {
			c.Header("Retry-After", "300")
			c.AbortWithStatusJSON(statusTooManyRequests, gin.H{"message": "登录尝试过多，请稍后再试"})
			return
		}
		c.Next()
	}
}

func (s *Server) requireAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionData, ok := sessionFromContext(c)
		if !ok {
			c.String(statusUnauthorized, "Unauthorized")
			c.Abort()
			return
		}
		c.Set("session", sessionData)
		c.Next()
	}
}

func (s *Server) requireWebAuth(c *gin.Context) bool {
	if _, ok := sessionFromContext(c); ok {
		return true
	}
	c.Redirect(statusSeeOther, "/login")
	return false
}

func sessionFromContext(c *gin.Context) (lib.SessionData, bool) {
	sid, err := c.Cookie(sessionCookieName)
	if err != nil || sid == "" {
		return lib.SessionData{}, false
	}
	data, ok := lib.GetSession(sid)
	if !ok {
		return lib.SessionData{}, false
	}
	settings, err := lib.GetSettings()
	if err != nil || data.CredentialsVersion != lib.CredentialsVersion(settings) {
		lib.DeleteSession(sid)
		return lib.SessionData{}, false
	}
	return data, true
}

func (s *Server) createSessionCookie(c *gin.Context) error {
	sid, err := lib.GenerateSessionID()
	if err != nil {
		return err
	}
	userID, err := lib.RandUserID()
	if err != nil {
		return err
	}

	expiration := time.Now().Add(sessionTTL)
	credentials := &lib.Settings{Admin: c.PostForm("username"), Password: c.PostForm("password")}
	if err := lib.SetSession(sid, lib.SessionData{UserID: userID, ExpiresAt: expiration, CredentialsVersion: lib.CredentialsVersion(credentials)}); err != nil {
		return err
	}
	if previous, err := c.Cookie(sessionCookieName); err == nil {
		lib.DeleteSession(previous)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, sid, sessionMaxAge, "/", "", s.requestScheme(c) == "https", true)
	return nil
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	if sid, err := c.Cookie(sessionCookieName); err == nil && sid != "" {
		lib.DeleteSession(sid)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", s.requestScheme(c) == "https", true)
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

func (s *Server) renderTemplate(c *gin.Context, status int, name string, data any) {
	tmpl, err := template.ParseFS(s.files, templatePattern)
	if err != nil {
		c.String(statusInternalServerError, fmt.Sprintf("template parse error: %v", err))
		return
	}

	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, name, data); err != nil {
		c.String(statusInternalServerError, fmt.Sprintf("template render error: %v", err))
	}
}

func (s *Server) loadDashboardData(message string) (DashboardData, error) {
	cfg, err := lib.GetConfigSnapshot()
	if err != nil {
		return DashboardData{}, fmt.Errorf("load config: %w", err)
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		return DashboardData{}, fmt.Errorf("load subscription path: %w", err)
	}
	subURL, err := lib.GetSubscriptionURL()
	if err != nil {
		return DashboardData{}, fmt.Errorf("build subscription url: %w", err)
	}
	rules, err := lib.GetRouteRules()
	if err != nil {
		return DashboardData{}, fmt.Errorf("load route rules: %w", err)
	}
	_, singBoxReport, singBoxErr := lib.GetSingBoxSubscription()
	if singBoxErr != nil {
		singBoxReport = lib.ConversionReport{Status: "error", Diagnostics: []lib.ConversionDiagnostic{{Code: "EXPORT_STORAGE_ERROR", Status: "invalid", Message: "sing-box 导出暂时不可用"}}}
	}
	singBoxMode := "tun"
	if cfg.Settings.SingBox != nil && cfg.Settings.SingBox.Mode != "" {
		singBoxMode = cfg.Settings.SingBox.Mode
	}

	tlsStatus := "关闭"
	if cfg.Settings.TLS != nil && cfg.Settings.TLS.Enabled {
		tlsStatus = "开启"
	}
	routeRules := make([]RouteRule, 0, len(rules))
	for _, rule := range rules {
		routeRules = append(routeRules, RouteRule{
			ID:      rule.ID,
			Name:    rule.Name,
			Path:    rule.Path,
			URL:     publicURL(cfg.Settings, rule.Path),
			Content: rule.Content,
		})
	}
	routeRulesJSON, err := json.Marshal(routeRules)
	if err != nil {
		return DashboardData{}, fmt.Errorf("marshal route rules: %w", err)
	}

	return DashboardData{
		Settings:          cfg.Settings,
		ListenAddress:     net.JoinHostPort(cfg.Settings.Host, strconv.Itoa(cfg.Settings.Port)),
		TLSStatus:         tlsStatus,
		SubscriptionPath:  path,
		SubscriptionURL:   subURL,
		SingBoxPath:       lib.SingBoxPath(path),
		SingBoxURL:        publicURL(cfg.Settings, lib.SingBoxPath(path)),
		SingBoxReport:     singBoxReport,
		SingBoxStatusText: singBoxStatusText(singBoxReport.Status),
		SingBoxMode:       singBoxMode,
		NodesText:         strings.Join(cfg.Nodes, "\n"),
		NodeCount:         len(cfg.Nodes),
		RouteRules:        routeRules,
		RouteRulesData:    base64.StdEncoding.EncodeToString(routeRulesJSON),
		DefaultRouteJSON:  lib.DefaultRouteRuleJSON,
		Message:           message,
	}, nil
}

func singBoxStatusText(status string) string {
	switch status {
	case "ready":
		return "已同步"
	case "warning":
		return "已同步，存在跳过节点或转换警告"
	case "stale":
		return "未同步：保留上一次有效配置"
	case "disabled":
		return "已停用：节点列表为空"
	default:
		return "不可用：转换失败"
	}
}

func publicURL(settings *lib.Settings, path string) string {
	scheme := "http"
	if settings.TLS != nil && settings.TLS.Enabled {
		scheme = "https"
	}
	host := settings.Host
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(settings.Port)) + path
}

func wantsJSON(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "application/json")
}
