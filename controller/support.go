package controller

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

const (
	statusOK                  = 200
	statusSeeOther            = 303
	statusBadRequest          = 400
	statusUnauthorized        = 401
	statusForbidden           = 403
	statusNotFound            = 404
	statusMethodNotAllowed    = 405
	statusInternalServerError = 500
	statusServiceUnavailable  = 503
)

const (
	sessionCookieName = "session_id"
	sessionMaxAge     = 2160 * 60 * 60
	sessionTTL        = 2160 * time.Hour
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
		if !sameOriginRequest(c) {
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
		headers.Set("Referrer-Policy", "same-origin")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		if !strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			headers.Set("Cache-Control", "no-store")
		}
		if settings, err := lib.GetSettings(); err == nil && settings.TLS != nil && settings.TLS.Enabled {
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

func sameOriginRequest(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin != "" {
		return originMatchesHost(origin, c.Request.Host)
	}

	referer := c.GetHeader("Referer")
	if referer != "" {
		return originMatchesHost(referer, c.Request.Host)
	}

	return true
}

func originMatchesHost(rawURL, host string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
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
	return lib.GetSession(sid)
}

func createSessionCookie(c *gin.Context) error {
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

	expiration := time.Now().Add(sessionTTL)
	if err := lib.SetSession(sid, lib.SessionData{UserID: userID, ExpiresAt: expiration}); err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, sid, sessionMaxAge, "/", "", settings.TLS != nil && settings.TLS.Enabled, true)
	return nil
}

func clearSessionCookie(c *gin.Context) {
	if sid, err := c.Cookie(sessionCookieName); err == nil && sid != "" {
		lib.DeleteSession(sid)
	}
	secure := false
	if settings, err := lib.GetSettings(); err == nil {
		secure = settings.TLS != nil && settings.TLS.Enabled
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", secure, true)
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
