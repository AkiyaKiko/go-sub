package lib

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const SingBoxTemplateVersion = "security-template-v6"
const maxSingBoxNodes = 2048
const maxShareURIBytes = 16384
const maxSingBoxInputBytes = 4 * 1024 * 1024

type SingBoxOptions struct {
	Mode            string `json:"mode,omitempty"`
	AllowPartial    bool   `json:"allow_partial,omitempty"`
	MixedPort       int    `json:"mixed_port,omitempty"`
	ClashPort       int    `json:"clash_port,omitempty"`
	DisableClashAPI bool   `json:"disable_clash_api,omitempty"`
	TestURL         string `json:"test_url,omitempty"`
	TestInterval    string `json:"test_interval,omitempty"`
}

func NormalizeSingBoxOptions(options SingBoxOptions) (SingBoxOptions, error) {
	if options.Mode == "" {
		options.Mode = "tun"
	}
	if options.Mode != "tun" && options.Mode != "mixed" {
		return options, fmt.Errorf("singbox mode must be tun or mixed")
	}
	if options.MixedPort == 0 {
		options.MixedPort = 2080
	}
	if options.ClashPort == 0 {
		options.ClashPort = 9090
	}
	if options.MixedPort < 1 || options.MixedPort > 65535 || options.ClashPort < 1 || options.ClashPort > 65535 {
		return options, fmt.Errorf("singbox ports must be between 1 and 65535")
	}
	if options.Mode == "mixed" && !options.DisableClashAPI && options.MixedPort == options.ClashPort {
		return options, fmt.Errorf("singbox mixed and controller ports must differ")
	}
	if options.TestURL == "" {
		options.TestURL = "https://www.gstatic.com/generate_204"
	}
	parsed, err := url.Parse(options.TestURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return options, fmt.Errorf("singbox test_url must be an HTTP or HTTPS URL without credentials")
	}
	if options.TestInterval == "" {
		options.TestInterval = "10m"
	}
	interval, err := time.ParseDuration(options.TestInterval)
	if err != nil || interval < time.Minute {
		return options, fmt.Errorf("singbox test_interval must be at least 1m")
	}
	return options, nil
}

func SingBoxPath(subscriptionPath string) string {
	return subscriptionPath + "/sing-box"
}

type ConversionDiagnostic struct {
	SourceIndex int    `json:"source_index"`
	Status      string `json:"status"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

type ConversionReport struct {
	TemplateHash  string                 `json:"template_hash,omitempty"`
	Status        string                 `json:"status"`
	Total         int                    `json:"total"`
	Converted     int                    `json:"converted"`
	Failed        int                    `json:"failed"`
	Warnings      int                    `json:"warnings"`
	GeneratedAt   string                 `json:"generated_at,omitempty"`
	SourceHash    string                 `json:"source_hash,omitempty"`
	PublishedHash string                 `json:"published_hash,omitempty"`
	Diagnostics   []ConversionDiagnostic `json:"diagnostics"`
}

type shareNode struct {
	singBoxProtocolFields
	Protocol, Name, Server, UUID, Method, Password, Flow string
	Port                                                 int
	TLS                                                  *singBoxTLS
	Transport                                            *singBoxTransport
	PacketEncoding                                       *string
	Plugin, PluginOptions                                string
}

type singBoxTLS struct {
	Enabled    bool            `json:"enabled"`
	DisableSNI bool            `json:"disable_sni,omitempty"`
	ServerName string          `json:"server_name,omitempty"`
	Insecure   bool            `json:"insecure,omitempty"`
	ALPN       []string        `json:"alpn,omitempty"`
	UTLS       *singBoxUTLS    `json:"utls,omitempty"`
	Reality    *singBoxReality `json:"reality,omitempty"`
}

type singBoxUTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type singBoxReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type singBoxTransport struct {
	Type                string            `json:"type"`
	Path                string            `json:"path,omitempty"`
	Host                any               `json:"host,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	ServiceName         string            `json:"service_name,omitempty"`
	MaxEarlyData        int               `json:"max_early_data,omitempty"`
	EarlyDataHeaderName string            `json:"early_data_header_name,omitempty"`
}

type singBoxOutbound struct {
	singBoxProtocolFields
	Type           string            `json:"type"`
	Tag            string            `json:"tag"`
	Server         string            `json:"server,omitempty"`
	ServerPort     int               `json:"server_port,omitempty"`
	UUID           string            `json:"uuid,omitempty"`
	Flow           string            `json:"flow,omitempty"`
	Method         string            `json:"method,omitempty"`
	Password       string            `json:"password,omitempty"`
	TLS            *singBoxTLS       `json:"tls,omitempty"`
	Transport      *singBoxTransport `json:"transport,omitempty"`
	PacketEncoding *string           `json:"packet_encoding,omitempty"`
	Plugin         string            `json:"plugin,omitempty"`
	PluginOptions  string            `json:"plugin_opts,omitempty"`
	Outbounds      []string          `json:"outbounds,omitempty"`
	Default        string            `json:"default,omitempty"`
	URL            string            `json:"url,omitempty"`
	Interval       string            `json:"interval,omitempty"`
}

type singBoxProtocolFields struct {
	Security                 string   `json:"security,omitempty"`
	AlterID                  int      `json:"alter_id,omitempty"`
	Username                 string   `json:"username,omitempty"`
	Version                  string   `json:"version,omitempty"`
	AuthString               string   `json:"auth_str,omitempty"`
	Obfs                     any      `json:"obfs,omitempty"`
	UpMbps                   int      `json:"up_mbps,omitempty"`
	DownMbps                 int      `json:"down_mbps,omitempty"`
	ServerPorts              []string `json:"server_ports,omitempty"`
	HopInterval              string   `json:"hop_interval,omitempty"`
	CongestionControl        string   `json:"congestion_control,omitempty"`
	UDPRelayMode             string   `json:"udp_relay_mode,omitempty"`
	UDPOverStream            bool     `json:"udp_over_stream,omitempty"`
	ZeroRTTHandshake         bool     `json:"zero_rtt_handshake,omitempty"`
	Heartbeat                string   `json:"heartbeat,omitempty"`
	IdleSessionCheckInterval string   `json:"idle_session_check_interval,omitempty"`
	IdleSessionTimeout       string   `json:"idle_session_timeout,omitempty"`
	MinIdleSession           int      `json:"min_idle_session,omitempty"`
}

type singBoxObfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}

type shareError struct{ code, message string }

func (err *shareError) Error() string { return err.message }

func invalidShare(code, message string) error { return &shareError{code, message} }

func BuildSingBoxConfig(nodes []string, options SingBoxOptions) ([]byte, ConversionReport) {
	return buildSingBoxConfig(nodes, options, nil, false)
}

func BuildSingBoxConfigWithTemplate(nodes []string, options SingBoxOptions, template []byte) ([]byte, ConversionReport) {
	return buildSingBoxConfig(nodes, options, template, true)
}

func buildSingBoxConfig(nodes []string, options SingBoxOptions, template []byte, custom bool) ([]byte, ConversionReport) {
	report := ConversionReport{Status: "error", Diagnostics: []ConversionDiagnostic{}}
	inputBytes := 0
	for _, raw := range nodes {
		inputBytes += len(raw)
		if normalizeShareLine(raw) != "" {
			report.Total++
		}
	}
	if report.Total == 0 {
		report.Status = "disabled"
		return nil, report
	}
	var err error
	if !custom {
		template, err = DefaultSingBoxTemplate(options)
	}
	var config map[string]any
	if err == nil {
		config, err = parseSingBoxTemplate(template)
	}
	if err != nil {
		message := "sing-box 模板设置无效"
		if problem, ok := err.(*shareError); ok {
			message = problem.message
		}
		report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{Code: "INVALID_TEMPLATE", Status: "invalid", Message: message})
		return nil, report
	}
	report.TemplateHash = singBoxTemplateHash(template)
	if report.Total > maxSingBoxNodes || inputBytes > maxSingBoxInputBytes {
		report.Failed = report.Total
		report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{Code: "INPUT_LIMIT", Status: "invalid", Message: "sing-box 输入超过 2048 个节点或 4 MiB"})
		return nil, report
	}
	var outbounds []singBoxOutbound
	tags, _ := templateTags(config)
	for index, raw := range nodes {
		raw = normalizeShareLine(raw)
		if raw == "" {
			continue
		}
		node, warnings, err := parseShareNode(raw)
		if err != nil {
			problem, ok := err.(*shareError)
			if !ok {
				problem = &shareError{"INVALID_URI", "分享链接格式无效"}
			}
			status := "invalid"
			if strings.HasPrefix(problem.code, "UNSUPPORTED_") {
				status = "unsupported"
			}
			report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{index + 1, status, problem.code, problem.message})
			report.Failed++
			continue
		}
		for _, warning := range warnings {
			report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{index + 1, "warning", warning.code, warning.message})
			report.Warnings++
		}
		name := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, node.Name))
		nameRunes := []rune(name)
		if len(nameRunes) > 80 {
			name = string(nameRunes[:80])
		}
		if name == "" {
			prefix := strings.ToUpper(node.Protocol)
			if node.Protocol == "shadowsocks" {
				prefix = "SS"
			}
			name = fmt.Sprintf("%s-%d", prefix, index+1)
		}
		if tags[name] {
			fingerprint := sha256.Sum256([]byte(raw))
			base := fmt.Sprintf("%s-%x", name, fingerprint[:4])
			name = base
			for suffix := 2; tags[name]; suffix++ {
				name = fmt.Sprintf("%s-%d", base, suffix)
			}
		}
		tags[name] = true
		outbounds = append(outbounds, singBoxOutbound{
			singBoxProtocolFields: node.singBoxProtocolFields,
			Type:                  node.Protocol, Tag: name, Server: node.Server, ServerPort: node.Port,
			UUID: node.UUID, Flow: node.Flow, Method: node.Method, Password: node.Password,
			TLS: node.TLS, Transport: node.Transport, PacketEncoding: node.PacketEncoding,
			Plugin: node.Plugin, PluginOptions: node.PluginOptions,
		})
		report.Converted++
	}
	if report.Converted == 0 || (report.Failed > 0 && !options.AllowPartial) {
		return nil, report
	}
	body, err := assembleSingBoxTemplate(config, outbounds)
	if err != nil {
		message := "模板组装失败"
		if problem, ok := err.(*shareError); ok {
			message = problem.message
		}
		report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{Code: "INVALID_TEMPLATE", Status: "invalid", Message: message})
		return nil, report
	}
	report.Status = "ready"
	if report.Failed > 0 || report.Warnings > 0 {
		report.Status = "warning"
	}
	return body, report
}

func normalizeShareLine(raw string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
}

func parseShareNode(raw string) (shareNode, []shareError, error) {
	if len(raw) > maxShareURIBytes || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return shareNode{}, nil, invalidShare("INVALID_URI", "分享链接过长或包含非法字符")
	}
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return shareNode{}, nil, invalidShare("INVALID_URI", "分享链接缺少协议")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESS(raw)
	case "ss":
		node, err := parseShadowsocks(raw)
		return node, nil, err
	case "vmess":
		node, err := parseVMess(raw)
		return node, nil, err
	case "trojan", "anytls", "hysteria", "hy", "hysteria2", "hy2", "tuic", "socks", "socks5", "http", "https":
		node, err := parseProxyShare(raw, strings.ToLower(scheme))
		return node, nil, err
	default:
		return shareNode{}, nil, invalidShare("UNSUPPORTED_SCHEME", "sing-box 转换尚不支持该分享链接协议")
	}
}

func shareEndpoint(parsed *url.URL) (string, int, error) {
	host := parsed.Hostname()
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 || host == "" || strings.ContainsAny(host, " /\\\t\r\n@%") {
		return "", 0, invalidShare("INVALID_ENDPOINT", "服务器地址或端口无效")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", 0, invalidShare("INVALID_ENDPOINT", "IPv6 地址无效")
	}
	return host, port, nil
}

func shareQuery(raw string, allowed string) (url.Values, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return nil, invalidShare("INVALID_QUERY", "链接参数编码无效")
	}
	allow := make(map[string]bool)
	for _, key := range strings.Fields(allowed) {
		allow[key] = true
	}
	for key, values := range query {
		if !allow[key] {
			return nil, invalidShare("UNSUPPORTED_PARAMETER", "链接包含尚未支持的参数，未进行有损转换")
		}
		if len(values) != 1 || strings.ContainsAny(values[0], "\r\n\x00") {
			return nil, invalidShare("INVALID_QUERY", "链接参数重复或包含非法字符")
		}
	}
	return query, nil
}

func parseVLESS(raw string) (shareNode, []shareError, error) {
	var node shareNode
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil || (parsed.Path != "" && parsed.Path != "/") {
		return node, nil, invalidShare("INVALID_URI", "VLESS 分享链接格式无效")
	}
	node.Protocol, node.Name, node.UUID = "vless", parsed.Fragment, parsed.User.Username()
	if _, hasPassword := parsed.User.Password(); hasPassword || !validUUID(node.UUID) {
		return node, nil, invalidShare("INVALID_UUID", "VLESS UUID 格式无效")
	}
	node.Server, node.Port, err = shareEndpoint(parsed)
	if err != nil {
		return node, nil, err
	}
	query, err := shareQuery(parsed.RawQuery, "encryption security type flow sni fp pbk sid alpn allowInsecure insecure host path serviceName mode authority headerType packetEncoding spx ed eh quicSecurity key")
	if err != nil {
		return node, nil, err
	}
	if encryption := query.Get("encryption"); encryption != "" && encryption != "none" {
		return node, nil, invalidShare("UNSUPPORTED_ENCRYPTION", "不支持该 VLESS encryption 参数")
	}
	node.Flow = query.Get("flow")
	if node.Flow != "" && node.Flow != "xtls-rprx-vision" {
		return node, nil, invalidShare("UNSUPPORTED_FLOW", "不支持该 VLESS flow")
	}
	if value, ok := query["packetEncoding"]; ok {
		encoding := value[0]
		if encoding == "none" {
			encoding = ""
		}
		if encoding != "" && encoding != "xudp" && encoding != "packetaddr" {
			return node, nil, invalidShare("UNSUPPORTED_PACKET_ENCODING", "不支持该 UDP 数据包编码")
		}
		node.PacketEncoding = &encoding
	}
	if err := applyShareTransport(&node, query); err != nil {
		return node, nil, err
	}
	if node.Flow != "" && (node.TLS == nil || node.Transport != nil) {
		return node, nil, invalidShare("UNSUPPORTED_FLOW_TRANSPORT", "Vision 仅支持 TLS/Reality 的普通 TCP 传输")
	}
	warnings := []shareError{}
	if query.Get("spx") != "" {
		if node.TLS == nil || node.TLS.Reality == nil {
			return node, nil, invalidShare("INVALID_REALITY", "spx 仅适用于 Reality")
		}
		warnings = append(warnings, shareError{"IGNORED_REALITY_SPIDER_X", "sing-box 不使用 Xray 的 Reality spiderX 探测路径"})
	}
	return node, warnings, nil
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for _, i := range []int{8, 13, 18, 23} {
		if value[i] != '-' {
			return false
		}
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

func parseVLESSTLS(query url.Values) (*singBoxTLS, error) {
	security := query.Get("security")
	if security == "" || security == "none" {
		for _, key := range []string{"sni", "fp", "pbk", "sid", "alpn", "allowInsecure", "insecure"} {
			if (key == "allowInsecure" || key == "insecure") && query.Has(key) {
				value, err := strconv.ParseBool(query.Get(key))
				if err != nil || value {
					return nil, invalidShare("INVALID_TLS", "未启用 TLS 时不能设置跳过证书校验")
				}
				continue
			}
			if query.Get(key) != "" {
				return nil, invalidShare("INVALID_TLS", "TLS 参数需要明确启用 TLS 或 Reality")
			}
		}
		return nil, nil
	}
	if security != "tls" && security != "reality" {
		return nil, invalidShare("UNSUPPORTED_SECURITY", "不支持该 TLS 安全类型")
	}
	tls := &singBoxTLS{Enabled: true, ServerName: query.Get("sni")}
	var insecure *bool
	for _, key := range []string{"allowInsecure", "insecure"} {
		if raw, ok := query[key]; ok {
			value, err := strconv.ParseBool(raw[0])
			if err != nil || (insecure != nil && *insecure != value) {
				return nil, invalidShare("INVALID_TLS", "TLS 证书校验参数无效或冲突")
			}
			insecure = &value
			tls.Insecure = value
		}
	}
	if alpn := query.Get("alpn"); alpn != "" {
		for _, protocol := range strings.Split(alpn, ",") {
			protocol = strings.TrimSpace(protocol)
			if protocol == "" || len(protocol) > 255 {
				return nil, invalidShare("INVALID_ALPN", "ALPN 参数无效")
			}
			tls.ALPN = append(tls.ALPN, protocol)
		}
	}
	fingerprint := query.Get("fp")
	if fingerprint != "" {
		switch fingerprint {
		case "chrome", "firefox", "edge", "safari", "360", "qq", "ios", "android", "random", "randomized":
		default:
			return nil, invalidShare("UNSUPPORTED_TLS_FINGERPRINT", "sing-box 不支持该 TLS 指纹")
		}
		tls.UTLS = &singBoxUTLS{Enabled: true, Fingerprint: fingerprint}
	}
	if security == "reality" {
		key, err := base64.RawURLEncoding.Strict().DecodeString(query.Get("pbk"))
		if err != nil || len(key) != 32 {
			return nil, invalidShare("INVALID_REALITY_PUBLIC_KEY", "Reality 公钥缺失或格式无效")
		}
		shortID := query.Get("sid")
		if _, err := hex.DecodeString(shortID); err != nil || len(shortID) > 16 {
			return nil, invalidShare("INVALID_REALITY_SHORT_ID", "Reality short_id 必须为最多 8 字节的十六进制字符串")
		}
		if tls.ServerName == "" {
			return nil, invalidShare("MISSING_REALITY_SNI", "Reality 需要提供 SNI")
		}
		if tls.UTLS == nil {
			tls.UTLS = &singBoxUTLS{Enabled: true}
		}
		tls.Reality = &singBoxReality{Enabled: true, PublicKey: query.Get("pbk"), ShortID: shortID}
	} else if query.Get("pbk") != "" || query.Get("sid") != "" {
		return nil, invalidShare("INVALID_REALITY", "Reality 参数不能用于普通 TLS")
	}
	return tls, nil
}

func parseVLESSTransport(query url.Values) (*singBoxTransport, error) {
	kind := query.Get("type")
	if kind == "xhttp" || kind == "splithttp" {
		return nil, invalidShare("UNSUPPORTED_TRANSPORT_XHTTP", "sing-box 不支持 XHTTP/SplitHTTP，未降级为 TCP")
	}
	if header := query.Get("headerType"); header != "" && header != "none" {
		return nil, invalidShare("UNSUPPORTED_HEADER_TYPE", "不支持 Xray 的传输头伪装")
	}
	switch kind {
	case "", "tcp":
		for _, key := range []string{"host", "path", "serviceName", "mode", "authority", "ed", "eh", "quicSecurity", "key"} {
			if query.Get(key) != "" {
				return nil, invalidShare("UNSUPPORTED_TRANSPORT_PARAMETER", "普通 TCP 含有无法等价转换的传输参数")
			}
		}
		return nil, nil
	case "ws", "http", "httpupgrade", "grpc", "quic":
	default:
		return nil, invalidShare("UNSUPPORTED_TRANSPORT", "sing-box 不支持该传输类型")
	}
	transport := &singBoxTransport{Type: kind}
	if kind != "quic" && (query.Get("quicSecurity") != "" || query.Get("key") != "") {
		return nil, invalidShare("UNSUPPORTED_TRANSPORT_PARAMETER", "传输参数与类型不匹配")
	}
	if kind != "grpc" && (query.Get("serviceName") != "" || query.Get("mode") != "" || query.Get("authority") != "") {
		return nil, invalidShare("UNSUPPORTED_TRANSPORT_PARAMETER", "传输参数与类型不匹配")
	}
	if kind == "grpc" {
		if query.Get("authority") != "" || (query.Get("mode") != "" && query.Get("mode") != "gun") || query.Get("host") != "" || query.Get("path") != "" {
			return nil, invalidShare("UNSUPPORTED_GRPC_MODE", "不支持该 gRPC mode、authority 或额外传输参数")
		}
		transport.ServiceName = query.Get("serviceName")
	} else if kind == "quic" {
		if (query.Get("quicSecurity") != "" && query.Get("quicSecurity") != "none") || query.Get("key") != "" || query.Get("host") != "" || query.Get("path") != "" {
			return nil, invalidShare("UNSUPPORTED_QUIC_OPTIONS", "不支持 QUIC 附加加密或伪装")
		}
	} else {
		transport.Path = query.Get("path")
		if host := query.Get("host"); host != "" {
			switch kind {
			case "ws":
				transport.Headers = map[string]string{"Host": host}
			case "http":
				transport.Host = strings.Split(host, ",")
			case "httpupgrade":
				transport.Host = host
			}
		}
	}
	if kind != "ws" && (query.Get("ed") != "" || query.Get("eh") != "") {
		return nil, invalidShare("UNSUPPORTED_EARLY_DATA", "Early Data 参数仅适用于 WebSocket")
	}
	if kind == "ws" {
		earlyData := query.Get("ed")
		pathURL, err := url.Parse(transport.Path)
		if err != nil {
			return nil, invalidShare("INVALID_TRANSPORT_PATH", "WebSocket 路径无效")
		}
		if pathURL.RawQuery != "" {
			pathQuery, err := url.ParseQuery(pathURL.RawQuery)
			if err != nil {
				return nil, invalidShare("INVALID_TRANSPORT_PATH", "WebSocket 路径参数无效")
			}
			if values, ok := pathQuery["ed"]; ok {
				if len(values) != 1 || (earlyData != "" && earlyData != values[0]) {
					return nil, invalidShare("INVALID_EARLY_DATA", "WebSocket Early Data 参数冲突")
				}
				earlyData = values[0]
				pathQuery.Del("ed")
				pathURL.RawQuery = pathQuery.Encode()
				transport.Path = pathURL.String()
			}
		}
		if earlyData != "" {
			value, err := strconv.Atoi(earlyData)
			if err != nil || value < 0 || value > 1048576 {
				return nil, invalidShare("INVALID_EARLY_DATA", "WebSocket Early Data 大小无效")
			}
			transport.MaxEarlyData = value
			transport.EarlyDataHeaderName = "Sec-WebSocket-Protocol"
		}
		if header := query.Get("eh"); header != "" {
			transport.EarlyDataHeaderName = header
		}
	}
	return transport, nil
}

func decodeShareBase64(value string) ([]byte, error) {
	if strings.ContainsAny(value, " \t\r\n") {
		return nil, invalidShare("INVALID_BASE64", "分享链接 Base64 无效")
	}
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.Strict().DecodeString(value); err == nil && utf8.Valid(decoded) {
			return decoded, nil
		}
	}
	return nil, invalidShare("INVALID_BASE64", "分享链接 Base64 无效")
}

func parseShadowsocks(raw string) (shareNode, error) {
	node := shareNode{Protocol: "shadowsocks"}
	_, payload, _ := strings.Cut(raw, "://")
	payload, fragment, _ := strings.Cut(payload, "#")
	var err error
	node.Name, err = url.PathUnescape(fragment)
	if err != nil {
		return node, invalidShare("INVALID_URI", "节点名称编码无效")
	}
	payload, rawQuery, _ := strings.Cut(payload, "?")
	query, err := shareQuery(rawQuery, "plugin")
	if err != nil {
		return node, err
	}
	legacy := !strings.Contains(payload, "@")
	if legacy {
		decoded, err := decodeShareBase64(payload)
		if err != nil {
			return node, err
		}
		payload = string(decoded)
	}
	at := strings.LastIndex(payload, "@")
	if at < 1 {
		return node, invalidShare("INVALID_URI", "Shadowsocks 分享链接缺少凭据或服务器")
	}
	userinfo, endpoint := payload[:at], payload[at+1:]
	parsed, err := url.Parse("ss://" + endpoint)
	if err != nil || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return node, invalidShare("INVALID_ENDPOINT", "Shadowsocks 服务器地址无效")
	}
	node.Server, node.Port, err = shareEndpoint(parsed)
	if err != nil {
		return node, err
	}
	if !legacy && !strings.Contains(userinfo, ":") {
		decoded, err := decodeShareBase64(userinfo)
		if err != nil {
			return node, err
		}
		userinfo = string(decoded)
	} else if !legacy {
		parsedUser, err := url.Parse("ss://" + userinfo + "@example.com:1")
		if err != nil || parsedUser.User == nil {
			return node, invalidShare("INVALID_CREDENTIALS", "Shadowsocks 凭据编码无效")
		}
		node.Method = parsedUser.User.Username()
		node.Password, _ = parsedUser.User.Password()
	}
	if node.Method == "" {
		node.Method, node.Password, _ = strings.Cut(userinfo, ":")
	}
	switch node.Method {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "none", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305", "aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb", "rc4-md5", "chacha20-ietf", "xchacha20":
	default:
		return node, invalidShare("UNSUPPORTED_SS_METHOD", "sing-box 不支持该 Shadowsocks 加密方法")
	}
	if node.Password == "" && node.Method != "none" {
		return node, invalidShare("MISSING_SS_PASSWORD", "Shadowsocks 密码不能为空")
	}
	if strings.HasPrefix(node.Method, "2022-") {
		if node.Method == "2022-blake3-chacha20-poly1305" && strings.Contains(node.Password, ":") {
			return node, invalidShare("UNSUPPORTED_SS2022_IDENTITY", "Shadowsocks 2022 ChaCha20 不支持多用户身份密钥链")
		}
		keySize := 32
		if node.Method == "2022-blake3-aes-128-gcm" {
			keySize = 16
		}
		for _, part := range strings.Split(node.Password, ":") {
			key, err := base64.StdEncoding.Strict().DecodeString(part)
			if err != nil || len(key) != keySize {
				return node, invalidShare("INVALID_SS2022_KEY", "Shadowsocks 2022 密钥长度或编码无效")
			}
		}
	}
	if plugin := query.Get("plugin"); plugin != "" {
		node.Plugin, node.PluginOptions, _ = strings.Cut(plugin, ";")
		if node.Plugin != "obfs-local" && node.Plugin != "v2ray-plugin" {
			return node, invalidShare("UNSUPPORTED_SS_PLUGIN", "sing-box 不支持该 Shadowsocks 插件")
		}
		if err := validateSSPlugin(node.Plugin, node.PluginOptions); err != nil {
			return node, err
		}
	}
	return node, nil
}

func validateSSPlugin(plugin, options string) error {
	allowed := map[string]bool{}
	keys := "mode host path tls"
	if plugin == "obfs-local" {
		keys = "obfs obfs-host"
	}
	for _, key := range strings.Fields(keys) {
		allowed[key] = true
	}
	seen := map[string]bool{}
	mode := ""
	for _, part := range strings.Split(options, ";") {
		if part == "" {
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		if !allowed[key] || seen[key] {
			return invalidShare("UNSUPPORTED_SS_PLUGIN_OPTIONS", "插件选项不兼容或重复")
		}
		if key == "tls" && hasValue {
			return invalidShare("UNSUPPORTED_SS_PLUGIN_OPTIONS", "插件 TLS 开关仅支持无赋值的 SIP003 标志")
		}
		seen[key] = true
		if key == "obfs" || key == "mode" {
			mode = value
		}
	}
	if plugin == "obfs-local" && mode != "http" && mode != "tls" {
		return invalidShare("UNSUPPORTED_SS_PLUGIN_OPTIONS", "obfs-local 需要明确使用 http 或 tls 模式")
	}
	if plugin == "v2ray-plugin" && mode != "" && mode != "websocket" {
		return invalidShare("UNSUPPORTED_SS_PLUGIN_OPTIONS", "仅支持 v2ray-plugin WebSocket 模式")
	}
	return nil
}

const shareTLSKeys = "security sni peer alpn fp insecure allowInsecure allow-insecure allow_insecure skip-cert-verify"
const shareTransportKeys = "type host path serviceName mode authority headerType ed eh quicSecurity key"

func proxyShareQuery(raw, allowed string) (url.Values, error) {
	query, err := shareQuery(raw, allowed)
	if err != nil {
		return nil, err
	}
	aliases := map[string]string{
		"peer": "sni", "allow-insecure": "allowInsecure", "allow_insecure": "allowInsecure",
		"skip-cert-verify": "insecure", "congestion_control": "congestion-control",
		"udp_relay_mode": "udp-relay-mode", "udp_over_stream": "udp-over-stream",
		"disable_sni": "disable-sni", "reduce_rtt": "reduce-rtt", "hop_interval": "hop-interval",
	}
	for alias, canonical := range aliases {
		values, exists := query[alias]
		if !exists {
			continue
		}
		if err := setShareValue(query, canonical, values[0]); err != nil {
			return nil, err
		}
		query.Del(alias)
	}
	return query, nil
}

func applyShareTransport(node *shareNode, query url.Values) error {
	var err error
	node.Transport, err = parseVLESSTransport(query)
	if err != nil {
		return err
	}
	node.TLS, err = parseVLESSTLS(query)
	if err != nil {
		return err
	}
	if node.Transport != nil && node.Transport.Type == "quic" {
		if node.TLS == nil {
			return invalidShare("MISSING_QUIC_TLS", "sing-box QUIC 传输必须启用 TLS")
		}
		if node.TLS.UTLS != nil || node.TLS.Reality != nil {
			return invalidShare("UNSUPPORTED_TLS_TRANSPORT", "QUIC 传输不支持 uTLS 或 Reality")
		}
	}
	return nil
}

func parseVMess(raw string) (shareNode, error) {
	node := shareNode{Protocol: "vmess"}
	_, payload, _ := strings.Cut(raw, "://")
	payload, fragment, _ := strings.Cut(payload, "#")
	var query url.Values
	var err error
	if strings.Contains(payload, "@") {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.User == nil || (parsed.Path != "" && parsed.Path != "/") {
			return node, invalidShare("INVALID_URI", "VMess URL 格式无效")
		}
		if _, exists := parsed.User.Password(); exists {
			return node, invalidShare("INVALID_CREDENTIALS", "VMess URL 的用户信息应只包含 UUID")
		}
		node.UUID, node.Name = parsed.User.Username(), parsed.Fragment
		node.Server, node.Port, err = shareEndpoint(parsed)
		if err != nil {
			return node, err
		}
		query, err = proxyShareQuery(parsed.RawQuery, shareTLSKeys+" "+shareTransportKeys+" encryption alterId aid packetEncoding")
		if err != nil {
			return node, err
		}
		node.Security = query.Get("encryption")
		if err := setShareAlias(query, "alterId", "aid"); err != nil {
			return node, err
		}
	} else {
		decoded, err := decodeShareBase64(payload)
		if err != nil {
			return node, err
		}
		fields, err := decodeVMessObject(decoded)
		if err != nil {
			return node, err
		}
		if fields.Get("vcn") != "" || fields.Get("pcs") != "" {
			return node, invalidShare("UNSUPPORTED_PARAMETER", "暂不支持 VMess 的独立证书验证域名或证书固定参数")
		}
		node.Name, node.Server, node.UUID = fields.Get("ps"), fields.Get("add"), fields.Get("id")
		node.Port, err = shareInteger(fields.Get("port"), 1, 65535)
		if err != nil {
			return node, err
		}
		if _, _, err := shareEndpoint(&url.URL{Host: net.JoinHostPort(node.Server, strconv.Itoa(node.Port))}); err != nil {
			return node, err
		}
		if version := fields.Get("v"); version != "" && version != "2" {
			return node, invalidShare("UNSUPPORTED_VMESS_FORMAT", "仅支持 VMess v2 JSON 分享格式")
		}
		query = url.Values{}
		for _, key := range strings.Fields("sni alpn fp insecure allowInsecure peer packetEncoding mode authority serviceName") {
			if value, ok := fields[key]; ok {
				query[key] = value
			}
		}
		if value, exists := fields["verify_cert"]; exists {
			verify, err := strconv.ParseBool(value[0])
			if err != nil {
				return node, invalidShare("INVALID_TLS", "VMess 证书校验参数无效")
			}
			if err := setShareValue(query, "allowInsecure", strconv.FormatBool(!verify)); err != nil {
				return node, err
			}
		}
		switch fields.Get("tls") {
		case "", "none", "false", "0":
		case "tls", "true", "1":
			query.Set("security", "tls")
		default:
			return node, invalidShare("UNSUPPORTED_SECURITY", "不支持该 VMess TLS 安全类型")
		}
		kind := fields.Get("net")
		if kind == "h2" {
			if query.Get("security") != "tls" {
				return node, invalidShare("INVALID_TLS", "VMess HTTP/2 传输需要 TLS")
			}
			kind = "http"
		}
		query.Set("type", kind)
		query.Set("host", fields.Get("host"))
		query.Set("path", fields.Get("path"))
		header := fields.Get("type")
		if kind == "grpc" {
			if header != "" && header != "none" {
				if err := setShareValue(query, "mode", header); err != nil {
					return node, err
				}
			}
			if path := fields.Get("path"); path != "" {
				if err := setShareValue(query, "serviceName", path); err != nil {
					return node, err
				}
			}
			query.Del("path")
		} else {
			query.Set("headerType", header)
			if kind == "quic" {
				query.Set("quicSecurity", fields.Get("host"))
				query.Set("key", fields.Get("path"))
				query.Del("host")
				query.Del("path")
			}
		}
		query, err = proxyShareQuery(query.Encode(), shareTLSKeys+" "+shareTransportKeys+" packetEncoding")
		if err != nil {
			return node, err
		}
		query.Set("aid", fields.Get("aid"))
		node.Security = fields.Get("scy")
		if fragment != "" {
			node.Name, err = url.PathUnescape(fragment)
			if err != nil {
				return node, invalidShare("INVALID_URI", "节点名称编码无效")
			}
		}
	}
	if !validUUID(node.UUID) {
		return node, invalidShare("INVALID_UUID", "VMess UUID 格式无效")
	}
	if node.Security == "" {
		node.Security = "auto"
	}
	switch node.Security {
	case "auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305", "aes-128-ctr":
	default:
		return node, invalidShare("UNSUPPORTED_VMESS_SECURITY", "sing-box 不支持该 VMess 加密方法")
	}
	if query.Get("aid") != "" {
		node.AlterID, err = shareInteger(query.Get("aid"), 0, 65535)
		if err != nil {
			return node, err
		}
	}
	if value, ok := query["packetEncoding"]; ok {
		encoding := value[0]
		if encoding == "none" {
			encoding = ""
		}
		if encoding != "" && encoding != "xudp" && encoding != "packetaddr" {
			return node, invalidShare("UNSUPPORTED_PACKET_ENCODING", "不支持该 UDP 数据包编码")
		}
		node.PacketEncoding = &encoding
	}
	err = applyShareTransport(&node, query)
	return node, err
}

// Read scalar fields explicitly so duplicate keys and unknown connection options cannot be lost.
func decodeVMessObject(data []byte) (url.Values, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalidShare("INVALID_VMESS_JSON", "VMess Base64 内容不是 JSON 对象")
	}
	fields := url.Values{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields.Has(key) {
			return nil, invalidShare("INVALID_VMESS_JSON", "VMess JSON 字段重复或格式无效")
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, invalidShare("INVALID_VMESS_JSON", "VMess JSON 字段格式无效")
		}
		switch scalar := value.(type) {
		case string:
			fields.Set(key, scalar)
		case json.Number:
			fields.Set(key, scalar.String())
		case bool:
			fields.Set(key, strconv.FormatBool(scalar))
		default:
			return nil, invalidShare("INVALID_VMESS_JSON", "VMess JSON 字段类型无效")
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, invalidShare("INVALID_VMESS_JSON", "VMess JSON 格式无效")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalidShare("INVALID_VMESS_JSON", "VMess JSON 存在多余内容")
	}
	return shareQuery(fields.Encode(), "v ps add port id aid scy net type host path tls sni alpn fp insecure allowInsecure peer verify_cert packetEncoding mode authority serviceName vcn pcs")
}

func setShareValue(query url.Values, key, value string) error {
	if previous, ok := query[key]; ok && previous[0] != value {
		left, leftErr := strconv.ParseBool(previous[0])
		right, rightErr := strconv.ParseBool(value)
		if (key != "allowInsecure" && key != "insecure") || leftErr != nil || rightErr != nil || left != right {
			return invalidShare("INVALID_QUERY", "链接的同义参数存在冲突")
		}
	}
	query.Set(key, value)
	return nil
}

func setShareAlias(query url.Values, alias, canonical string) error {
	if values, exists := query[alias]; exists {
		if err := setShareValue(query, canonical, values[0]); err != nil {
			return err
		}
		query.Del(alias)
	}
	return nil
}

func shareInteger(raw string, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, invalidShare("INVALID_NUMBER", "链接数值参数缺失或超出范围")
	}
	return value, nil
}

func shareBoolean(query url.Values, key string) (bool, error) {
	if !query.Has(key) {
		return false, nil
	}
	value, err := strconv.ParseBool(query.Get(key))
	if err != nil {
		return false, invalidShare("INVALID_BOOLEAN", "链接开关参数格式无效")
	}
	return value, nil
}

func shareDuration(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if _, err := strconv.ParseUint(raw, 10, 32); err == nil {
		raw += "s"
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return "", invalidShare("INVALID_DURATION", "链接时间间隔必须为正数")
	}
	return value.String(), nil
}

func parseProxyShare(raw, scheme string) (shareNode, error) {
	node := shareNode{Protocol: scheme}
	switch scheme {
	case "hy":
		node.Protocol = "hysteria"
	case "hy2":
		node.Protocol = "hysteria2"
	case "socks", "socks5":
		node.Protocol, node.Version = "socks", "5"
	case "https":
		node.Protocol = "http"
	}
	defaultPort := 443
	if scheme == "http" {
		defaultPort = 80
	} else if node.Protocol == "socks" {
		defaultPort = 1080
	}
	parsed, ports, err := parseProxyEndpoint(raw, defaultPort, node.Protocol == "hysteria2" || node.Protocol == "hysteria")
	if err != nil {
		return node, err
	}
	node.Name = parsed.Fragment
	node.Server, node.Port, err = shareEndpoint(parsed)
	if err != nil {
		return node, err
	}
	if parsed.User != nil {
		node.Password = parsed.User.Username()
		if password, hasPassword := parsed.User.Password(); hasPassword {
			if node.Protocol == "tuic" || node.Protocol == "socks" || node.Protocol == "http" {
				node.Username, node.Password = parsed.User.Username(), password
			} else {
				node.Password += ":" + password
			}
		} else if node.Protocol == "tuic" || node.Protocol == "socks" || node.Protocol == "http" {
			node.Username, node.Password = parsed.User.Username(), ""
		}
	}
	allowed := shareTLSKeys
	switch node.Protocol {
	case "trojan":
		allowed += " " + shareTransportKeys
	case "anytls":
		allowed += " idle-session-check-interval idle-session-timeout min-idle-session"
	case "hysteria", "hysteria2":
		allowed += " obfs upmbps downmbps mport hop-interval hop_interval"
		if node.Protocol == "hysteria" {
			allowed += " auth obfsParam protocol"
		} else {
			allowed += " obfs-password"
		}
	case "tuic":
		allowed += " congestion-control congestion_control udp-relay-mode udp_relay_mode udp-over-stream udp_over_stream disable-sni disable_sni reduce-rtt reduce_rtt heartbeat"
	}
	query, err := proxyShareQuery(parsed.RawQuery, allowed)
	if err != nil {
		return node, err
	}
	if !query.Has("security") && (node.Protocol != "http" && node.Protocol != "socks" || scheme == "https") {
		query.Set("security", "tls")
	}
	if node.Protocol == "socks" || scheme == "http" {
		if query.Get("security") != "" && query.Get("security") != "none" {
			return node, invalidShare("UNSUPPORTED_SECURITY", "该代理链接不支持启用 TLS")
		}
	} else if node.Protocol != "trojan" && query.Get("security") != "tls" {
		return node, invalidShare("UNSUPPORTED_SECURITY", "该协议必须使用普通 TLS")
	}
	if err := applyShareTransport(&node, query); err != nil {
		return node, err
	}
	if node.Protocol == "hysteria" || node.Protocol == "hysteria2" || node.Protocol == "tuic" {
		if node.TLS.UTLS != nil {
			return node, invalidShare("UNSUPPORTED_TLS_TRANSPORT", "QUIC 协议不支持 uTLS 指纹")
		}
	}
	switch node.Protocol {
	case "trojan", "anytls":
		if node.Password == "" {
			return node, invalidShare("MISSING_PASSWORD", "协议密码不能为空")
		}
		if node.Protocol == "anytls" {
			node.IdleSessionCheckInterval, err = shareDuration(query.Get("idle-session-check-interval"))
			if err != nil {
				return node, err
			}
			node.IdleSessionTimeout, err = shareDuration(query.Get("idle-session-timeout"))
			if err != nil {
				return node, err
			}
			if query.Has("min-idle-session") {
				node.MinIdleSession, err = shareInteger(query.Get("min-idle-session"), 0, 65535)
			}
		}
	case "tuic":
		node.UUID, node.Username = node.Username, ""
		if !validUUID(node.UUID) || parsed.User == nil {
			return node, invalidShare("INVALID_UUID", "仅支持具有 UUID 的 TUIC v5 分享链接")
		}
		if _, ok := parsed.User.Password(); !ok {
			return node, invalidShare("INVALID_CREDENTIALS", "TUIC v5 链接需要 UUID:密码")
		}
		node.CongestionControl = query.Get("congestion-control")
		if node.CongestionControl != "" && node.CongestionControl != "cubic" && node.CongestionControl != "new_reno" && node.CongestionControl != "bbr" {
			return node, invalidShare("UNSUPPORTED_CONGESTION_CONTROL", "不支持该 TUIC 拥塞控制算法")
		}
		node.UDPRelayMode = query.Get("udp-relay-mode")
		if node.UDPRelayMode != "" && node.UDPRelayMode != "native" && node.UDPRelayMode != "quic" {
			return node, invalidShare("UNSUPPORTED_UDP_RELAY_MODE", "不支持该 TUIC UDP 模式")
		}
		node.UDPOverStream, err = shareBoolean(query, "udp-over-stream")
		if err != nil {
			return node, err
		}
		if node.UDPOverStream && node.UDPRelayMode != "" {
			return node, invalidShare("INVALID_QUERY", "TUIC 两种 UDP 转发配置不能同时使用")
		}
		node.TLS.DisableSNI, err = shareBoolean(query, "disable-sni")
		if err != nil {
			return node, err
		}
		node.ZeroRTTHandshake, err = shareBoolean(query, "reduce-rtt")
		if err != nil {
			return node, err
		}
		node.Heartbeat, err = shareDuration(query.Get("heartbeat"))
	case "hysteria", "hysteria2":
		if query.Has("mport") {
			queryPorts, err := parseSharePorts(query.Get("mport"))
			if err != nil {
				return node, err
			}
			if len(ports) > 0 && strings.Join(ports, ",") != strings.Join(queryPorts, ",") {
				return node, invalidShare("INVALID_QUERY", "端口跳跃配置存在冲突")
			}
			ports = queryPorts
		}
		node.ServerPorts = ports
		if len(ports) > 0 {
			node.Port = 0
		}
		node.HopInterval, err = shareDuration(query.Get("hop-interval"))
		if err != nil {
			return node, err
		}
		if node.HopInterval != "" && len(ports) == 0 {
			return node, invalidShare("INVALID_QUERY", "端口跳跃间隔需要提供多端口配置")
		}
		if query.Has("upmbps") {
			node.UpMbps, err = shareInteger(query.Get("upmbps"), 1, 1000000)
			if err != nil {
				return node, err
			}
		}
		if query.Has("downmbps") {
			node.DownMbps, err = shareInteger(query.Get("downmbps"), 1, 1000000)
			if err != nil {
				return node, err
			}
		}
		if node.Protocol == "hysteria" {
			if parsed.User != nil {
				return node, invalidShare("INVALID_CREDENTIALS", "Hysteria 1 的凭据应放在 auth 参数中")
			}
			if node.UpMbps == 0 || node.DownMbps == 0 {
				return node, invalidShare("MISSING_BANDWIDTH", "Hysteria 1 必须提供 upmbps 和 downmbps")
			}
			if protocol := query.Get("protocol"); protocol != "" && protocol != "udp" {
				return node, invalidShare("UNSUPPORTED_HYSTERIA_PROTOCOL", "sing-box Hysteria 1 仅支持 UDP 传输")
			}
			if obfs := query.Get("obfs"); obfs != "" && obfs != "xplus" {
				return node, invalidShare("UNSUPPORTED_OBFS", "Hysteria 1 仅支持 xplus 混淆")
			}
			if (query.Get("obfs") == "xplus") != (query.Get("obfsParam") != "") {
				return node, invalidShare("INVALID_OBFS", "Hysteria 1 混淆类型和密码必须成对提供")
			}
			node.AuthString = query.Get("auth")
			if query.Get("obfsParam") != "" {
				node.Obfs = query.Get("obfsParam")
			}
		} else {
			obfs := query.Get("obfs")
			if obfs != "" && obfs != "none" && obfs != "salamander" && obfs != "gecko" {
				return node, invalidShare("UNSUPPORTED_OBFS", "不支持该 Hysteria 2 混淆类型")
			}
			if (obfs == "salamander" || obfs == "gecko") != (query.Get("obfs-password") != "") {
				return node, invalidShare("INVALID_OBFS", "Hysteria 2 混淆类型和密码必须成对提供")
			}
			if obfs == "salamander" || obfs == "gecko" {
				node.Obfs = &singBoxObfs{Type: obfs, Password: query.Get("obfs-password")}
			}
		}
	}
	return node, err
}

func parseProxyEndpoint(raw string, defaultPort int, allowHopping bool) (*url.URL, []string, error) {
	var ports []string
	if allowHopping {
		scheme, rest, _ := strings.Cut(raw, "://")
		end := strings.IndexAny(rest, "/?#")
		if end < 0 {
			end = len(rest)
		}
		authority := rest[:end]
		hostStart := strings.LastIndex(authority, "@") + 1
		colon := strings.LastIndex(authority, ":")
		if colon >= hostStart && !strings.Contains(authority[colon:], "]") {
			port := authority[colon+1:]
			if strings.ContainsAny(port, ",;-") {
				var err error
				ports, err = parseSharePorts(port)
				if err != nil {
					return nil, nil, err
				}
				raw = scheme + "://" + authority[:colon+1] + strconv.Itoa(defaultPort) + rest[end:]
			}
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, nil, invalidShare("INVALID_URI", "代理分享链接格式无效")
	}
	if parsed.Port() == "" {
		if strings.HasSuffix(parsed.Host, ":") {
			return nil, nil, invalidShare("INVALID_ENDPOINT", "服务器端口不能为空")
		}
		parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(defaultPort))
	}
	return parsed, ports, nil
}

func parseSharePorts(raw string) ([]string, error) {
	parts := strings.Split(strings.ReplaceAll(raw, ";", ","), ",")
	if len(parts) > 128 {
		return nil, invalidShare("INVALID_PORT_RANGE", "端口跳跃列表为空、过长或格式无效")
	}
	ports := make([]string, 0, len(parts))
	for _, part := range parts {
		low, high, isRange := strings.Cut(part, "-")
		start, err := shareInteger(low, 1, 65535)
		if err != nil {
			return nil, invalidShare("INVALID_PORT_RANGE", "端口跳跃范围无效")
		}
		if isRange {
			end, err := shareInteger(high, start, 65535)
			if err != nil {
				return nil, invalidShare("INVALID_PORT_RANGE", "端口跳跃范围无效")
			}
			ports = append(ports, fmt.Sprintf("%d:%d", start, end))
		} else {
			ports = append(ports, fmt.Sprintf("%d:%d", start, start))
		}
	}
	return ports, nil
}
