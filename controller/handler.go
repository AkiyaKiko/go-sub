package controller

import (
	"log"
	"strconv"
	"strings"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

func (s *Server) dashboard(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}
	s.renderDashboard(c, statusOK, flashMessage(c.Query("msg")))
}

func (s *Server) loginPage(c *gin.Context) {
	if _, ok := sessionFromContext(c); ok {
		c.Redirect(statusSeeOther, "/")
		return
	}
	s.renderTemplate(c, statusOK, "login.html", LoginData{Message: flashMessage(c.Query("msg"))})
}

func (s *Server) login(c *gin.Context) {
	if !lib.Authenticate(c.PostForm("username"), c.PostForm("password")) {
		c.Redirect(statusSeeOther, "/login?msg=login_failed")
		return
	}
	if err := s.createSessionCookie(c); err != nil {
		c.String(statusInternalServerError, "failed to create session")
		return
	}
	c.Redirect(statusSeeOther, "/?msg=login_ok")
}

func (s *Server) logout(c *gin.Context) {
	s.clearSessionCookie(c)
	c.Redirect(statusSeeOther, "/login?msg=logged_out")
}

func (s *Server) apiLogin(c *gin.Context) {
	if !lib.Authenticate(c.PostForm("username"), c.PostForm("password")) {
		c.String(statusUnauthorized, "Unauthorized")
		return
	}
	if err := s.createSessionCookie(c); err != nil {
		c.String(statusInternalServerError, "Internal Server Error")
		return
	}
	c.Status(statusOK)
}

func (s *Server) apiSession(c *gin.Context) {
	sessionData := c.MustGet("session").(lib.SessionData)
	c.JSON(statusOK, gin.H{
		"authenticated": true,
		"user_id":       sessionData.UserID,
	})
}

func (s *Server) apiLogout(c *gin.Context) {
	s.clearSessionCookie(c)
	c.Status(statusOK)
}

func (s *Server) apiConfig(c *gin.Context) {
	cfg, err := lib.GetConfigSnapshot()
	if err != nil {
		c.String(statusInternalServerError, "Failed to load config")
		return
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		c.String(statusInternalServerError, "Failed to load subscription path")
		return
	}
	subURL, err := lib.GetSubscriptionURL()
	if err != nil {
		c.String(statusInternalServerError, "Failed to build subscription URL")
		return
	}
	rules, err := lib.GetRouteRules()
	if err != nil {
		c.String(statusInternalServerError, "Failed to load route rules")
		return
	}

	cfg.Settings.Password = ""
	c.JSON(statusOK, gin.H{
		"settings":          cfg.Settings,
		"nodes":             cfg.Nodes,
		"subscription_path": path,
		"subscription_url":  subURL,
		"route_rules":       rules,
		"singbox_path":      lib.SingBoxPath(path),
		"singbox_url":       publicURL(cfg.Settings, lib.SingBoxPath(path)),
	})
}

func (s *Server) apiChangeURI(c *gin.Context) {
	newURI := strings.TrimSpace(c.PostForm("uri"))
	if err := lib.UpdateSubscriptionPath(newURI); err != nil {
		c.String(statusBadRequest, err.Error())
		return
	}
	c.Status(statusOK)
}

func (s *Server) apiChangeNodes(c *gin.Context) {
	if err := lib.UpdateNodes(c.PostFormArray("nodes")); err != nil {
		c.String(statusBadRequest, "节点保存失败：输入无效或存储不可用")
		return
	}
	c.Status(statusOK)
}

func (s *Server) saveSubscriptionPath(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	nextPath := strings.TrimSpace(c.PostForm("path"))
	if c.PostForm("submit_action") == "random" {
		randomPath, err := lib.RandomPublicPath()
		if err != nil {
			c.String(statusInternalServerError, "failed to generate path")
			return
		}
		nextPath = randomPath
	}
	if err := lib.UpdateSubscriptionPath(nextPath); err != nil {
		s.respondDashboard(c, statusBadRequest, publicPathErrorMessage(err, "订阅路径"))
		return
	}
	s.respondDashboard(c, statusOK, "订阅路径已保存")
}

func (s *Server) saveNodes(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	nodesText := strings.ReplaceAll(c.PostForm("nodes"), "\r\n", "\n")
	if err := lib.UpdateNodes(strings.Split(nodesText, "\n")); err != nil {
		c.String(statusInternalServerError, "failed to save nodes")
		return
	}
	s.respondDashboard(c, statusOK, "节点列表已保存")
}

func (s *Server) saveSingBoxSettings(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}
	modes := c.PostFormArray("mode")
	if len(modes) != 1 || (modes[0] != "tun" && modes[0] != "mixed") {
		s.respondDashboard(c, statusBadRequest, "请选择有效的 sing-box 运行模式")
		return
	}
	if err := lib.UpdateSingBoxMode(modes[0]); err != nil {
		s.respondDashboard(c, statusInternalServerError, "sing-box 设置保存失败，请检查配置文件及目录写入权限")
		return
	}
	settings, err := lib.GetSettings()
	if err != nil {
		s.respondDashboard(c, statusInternalServerError, "设置读取失败")
		return
	}
	preset, err := lib.DefaultSingBoxTemplate(*settings.SingBox)
	store, storeErr := lib.DefaultStore()
	if err != nil || storeErr != nil {
		s.respondDashboard(c, statusInternalServerError, "预设生成失败")
		return
	}
	if err := store.SaveSingBoxTemplate(preset); err != nil {
		s.respondDashboard(c, statusInternalServerError, "预设保存失败")
		return
	}
	s.respondDashboard(c, statusOK, "sing-box 设置已保存")
}

func (s *Server) apiSingBoxTemplate(c *gin.Context) {
	store, err := lib.DefaultStore()
	if err != nil {
		c.JSON(statusInternalServerError, gin.H{"message": "模板读取失败"})
		return
	}
	raw, err := store.SingBoxTemplate()
	message := ""
	if err != nil {
		message = "模板读取失败，可载入预设修复，或检查数据库状态"
	}
	tun, _ := lib.DefaultSingBoxTemplate(lib.SingBoxOptions{Mode: "tun"})
	mixed, _ := lib.DefaultSingBoxTemplate(lib.SingBoxOptions{Mode: "mixed"})
	c.JSON(statusOK, gin.H{"template": string(raw), "storage": "sqlite", "table": "singbox_template", "read_error": message, "presets": gin.H{"tun": string(tun), "mixed": string(mixed)}})
}

func (s *Server) saveSingBoxTemplate(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}
	store, err := lib.DefaultStore()
	if err != nil {
		s.respondDashboard(c, statusInternalServerError, "模板存储暂时不可用")
		return
	}
	if err := store.SaveSingBoxTemplate([]byte(c.PostForm("template"))); err != nil {
		message := "模板保存失败，请检查数据库状态及写入权限"
		status := statusInternalServerError
		if lib.IsSingBoxTemplateError(err) {
			message, status = err.Error(), statusBadRequest
		}
		s.respondDashboard(c, status, message)
		return
	}
	s.respondDashboard(c, statusOK, "sing-box 模板已保存")
}

func (s *Server) createRouteRule(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	path := strings.TrimSpace(c.PostForm("path"))
	if c.PostForm("submit_action") == "random" {
		randomPath, err := lib.RandomRouteRulePath()
		if err != nil {
			s.respondDashboard(c, statusInternalServerError, "生成路由规则路径失败")
			return
		}
		path = randomPath
	}

	if err := lib.CreateRouteRule(c.PostForm("name"), path, c.PostForm("content")); err != nil {
		s.respondDashboard(c, statusBadRequest, routeRuleErrorMessage(err))
		return
	}
	s.respondDashboard(c, statusOK, "路由规则已新增")
}

func (s *Server) updateRouteRule(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	id, ok := routeRuleID(c)
	if !ok {
		s.respondDashboard(c, statusBadRequest, "路由规则不存在")
		return
	}

	path := strings.TrimSpace(c.PostForm("path"))
	if c.PostForm("submit_action") == "random" {
		randomPath, err := lib.RandomRouteRulePath()
		if err != nil {
			s.respondDashboard(c, statusInternalServerError, "生成路由规则路径失败")
			return
		}
		path = randomPath
	}

	if err := lib.UpdateRouteRule(id, c.PostForm("name"), path, c.PostForm("content")); err != nil {
		s.respondDashboard(c, statusBadRequest, routeRuleErrorMessage(err))
		return
	}
	s.respondDashboard(c, statusOK, "路由规则已保存")
}

func (s *Server) deleteRouteRule(c *gin.Context) {
	if !s.requireWebAuth(c) {
		return
	}

	id, ok := routeRuleID(c)
	if !ok {
		s.respondDashboard(c, statusBadRequest, "路由规则不存在")
		return
	}
	if err := lib.DeleteRouteRule(id); err != nil {
		s.respondDashboard(c, statusBadRequest, routeRuleErrorMessage(err))
		return
	}
	s.respondDashboard(c, statusOK, "路由规则已删除")
}

func (s *Server) subscription(c *gin.Context) {
	settings, err := lib.GetSettings()
	if err != nil {
		c.String(statusInternalServerError, "config error")
		return
	}
	subscriptionPath, err := lib.GetSubscriptionPath()
	if err != nil {
		c.String(statusInternalServerError, "subscription path error")
		return
	}

	requestPath := c.Request.URL.Path
	isSingBox := requestPath == lib.SingBoxPath(subscriptionPath)
	if isSingBox {
		c.Set("private_subscription", true)
	}
	routeRule, routeRuleFound, err := lib.GetRouteRuleByPath(requestPath)
	if err != nil {
		c.String(statusInternalServerError, "route rule error")
		return
	}
	if requestPath != subscriptionPath && !routeRuleFound && !isSingBox {
		c.String(statusNotFound, "404 page not found")
		return
	}
	if c.Request.Method != "GET" && !(isSingBox && !routeRuleFound && c.Request.Method == "HEAD") {
		c.String(statusMethodNotAllowed, "Method not allowed")
		return
	}
	if isSingBox && !routeRuleFound {
		c.Set("private_subscription", true)
		s.singBoxSubscription(c)
		return
	}

	if settings.TLS != nil && settings.TLS.Enabled {
		c.Header("Strict-Transport-Security", "max-age=31536000")
	}
	if routeRuleFound {
		c.Data(statusOK, "application/json; charset=utf-8", []byte(routeRule.Content))
		return
	}

	body, err := lib.GetB64FromLib()
	if err != nil {
		log.Printf("get subscription data: %v", err)
		body = ""
	}
	c.Data(statusOK, "text/plain; charset=utf-8", []byte(body))
}

func (s *Server) singBoxSubscription(c *gin.Context) {
	body, report, err := lib.GetSingBoxSubscription()
	if err != nil || len(body) == 0 {
		c.Header("Retry-After", "300")
		if c.Request.Method == "HEAD" {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.Status(statusServiceUnavailable)
		} else {
			c.JSON(statusServiceUnavailable, gin.H{"message": "sing-box subscription unavailable"})
		}
		return
	}
	c.Header("X-SingBox-Status", report.Status)
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Length", strconv.Itoa(len(body)))
	if c.Request.Method == "HEAD" {
		c.Status(statusOK)
		return
	}
	c.Data(statusOK, "application/json; charset=utf-8", body)
}

func (s *Server) renderDashboard(c *gin.Context, status int, message string) {
	data, err := s.loadDashboardData(message)
	if err != nil {
		c.String(statusInternalServerError, "failed to load dashboard")
		return
	}

	s.renderTemplate(c, status, "dashboard.html", data)
}

func (s *Server) respondDashboard(c *gin.Context, status int, message string) {
	data, err := s.loadDashboardData(message)
	if err != nil {
		c.JSON(statusInternalServerError, gin.H{"message": "failed to load dashboard"})
		return
	}
	if wantsJSON(c) {
		c.JSON(status, data)
		return
	}
	s.renderTemplate(c, status, "dashboard.html", data)
}

func routeRuleID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func routeRuleErrorMessage(err error) string {
	return publicPathErrorMessage(err, "路由规则路径")
}

func publicPathErrorMessage(err error, field string) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "json is invalid"):
		return "路由规则 JSON 格式错误"
	case strings.Contains(msg, "json is too large"):
		return "路由规则 JSON 内容过大"
	case strings.Contains(msg, "must start with /"):
		return field + "必须以 / 开头"
	case strings.Contains(msg, "is required"):
		return field + "不能为空"
	case strings.Contains(msg, "must not contain whitespace"):
		return field + "不能包含空白字符"
	case strings.Contains(msg, "must not contain query or fragment"):
		return field + "不能包含 ? 或 #"
	case strings.Contains(msg, "too long"):
		return field + "过长"
	case strings.Contains(msg, "is reserved"):
		return field + "不能使用系统保留路径"
	case strings.Contains(msg, "path already used"):
		return field + "已经被占用"
	case strings.Contains(msg, "not found"):
		return "路由规则不存在"
	default:
		return msg
	}
}
