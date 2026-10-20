package lib

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"unicode/utf8"

	"git.local/gosub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxSingBoxTemplateBytes = 512 * 1024
const singBoxNodesPlaceholder = "{{nodes}}"

const singBoxTUNPreset = `{
  "log": { "level": "warn" },
  "dns": {
    "servers": [
      { "type": "udp", "tag": "dns-bootstrap", "server": "223.5.5.5", "server_port": 53 },
      {
        "type": "https", "tag": "dns-remote", "server": "1.1.1.1", "server_port": 443,
        "path": "/dns-query", "detour": "proxy",
        "tls": { "enabled": true, "server_name": "cloudflare-dns.com" }
      }
    ],
    "final": "dns-remote", "strategy": "prefer_ipv4", "independent_cache": true
  },
  "inbounds": [
    {
      "type": "tun", "tag": "tun-in",
      "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"],
      "mtu": 1500, "auto_route": true, "strict_route": true
    }
  ],
  "outbounds": [
    { "type": "selector", "tag": "proxy", "outbounds": ["auto", "direct", "{{nodes}}"], "default": "auto" },
    {
      "type": "urltest", "tag": "auto", "outbounds": ["{{nodes}}"],
      "url": "https://www.gstatic.com/generate_204", "interval": "10m"
    },
    { "type": "direct", "tag": "direct" }
  ],
  "route": {
    "rules": [ { "port": 53, "action": "hijack-dns" }, { "action": "sniff" } ],
    "auto_detect_interface": true, "default_domain_resolver": "dns-bootstrap", "final": "proxy"
  },
  "experimental": { "clash_api": { "external_controller": "127.0.0.1:9090" } }
}`

func DefaultSingBoxTemplate(options SingBoxOptions) ([]byte, error) {
	options, err := NormalizeSingBoxOptions(options)
	if err != nil {
		return nil, err
	}
	config, err := parseSingBoxTemplate([]byte(singBoxTUNPreset))
	if err != nil {
		return nil, err
	}
	groups := config["outbounds"].([]any)
	groups[1].(map[string]any)["url"] = options.TestURL
	groups[1].(map[string]any)["interval"] = options.TestInterval
	if options.Mode == "mixed" {
		config["inbounds"] = []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": options.MixedPort}}
		config["route"] = map[string]any{"final": "proxy"}
		delete(config, "dns")
	}
	if options.DisableClashAPI {
		delete(config, "experimental")
	} else {
		config["experimental"] = map[string]any{"clash_api": map[string]any{"external_controller": net.JoinHostPort("127.0.0.1", strconv.Itoa(options.ClashPort))}}
	}
	return json.MarshalIndent(config, "", "  ")
}

func templateError(message string) error {
	return invalidShare("INVALID_TEMPLATE", message)
}

func IsSingBoxTemplateError(err error) bool {
	var problem *shareError
	return errors.As(err, &problem) && problem.code == "INVALID_TEMPLATE"
}

func parseSingBoxTemplate(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > maxSingBoxTemplateBytes || !utf8.Valid(raw) {
		return nil, templateError("模板不能为空，且不能超过 512 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readTemplateJSON(decoder, 0)
	if err != nil {
		return nil, templateError("模板 JSON 格式错误、字段重复或嵌套过深")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, templateError("模板必须只包含一个 JSON 对象")
	}
	config, ok := value.(map[string]any)
	if !ok {
		return nil, templateError("模板顶层必须是 JSON 对象")
	}
	for _, key := range []string{"log", "dns", "ntp", "certificate", "route", "experimental"} {
		if value, exists := config[key]; exists {
			if _, ok := value.(map[string]any); !ok {
				return nil, templateError("模板的配置段必须使用 JSON 对象")
			}
		}
	}
	for _, key := range []string{"certificate_providers", "http_clients", "network_namespaces", "endpoints", "inbounds", "outbounds", "services"} {
		if value, exists := config[key]; exists {
			if _, ok := value.([]any); !ok {
				return nil, templateError("模板的列表配置段必须使用 JSON 数组")
			}
		}
	}
	if _, err := templateTags(config); err != nil {
		return nil, err
	}
	return config, nil
}

func readTemplateJSON(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON depth exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return nil, fmt.Errorf("invalid object key")
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate object key")
			}
			object[name], err = readTemplateJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
		}
		_, err = decoder.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for decoder.More() {
			value, err := readTemplateJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err = decoder.Token()
		return array, err
	default:
		return token, nil
	}
}

func templateTags(config map[string]any) (map[string]bool, error) {
	tags := map[string]bool{singBoxNodesPlaceholder: true}
	for _, key := range []string{"outbounds", "endpoints"} {
		items, _ := config[key].([]any)
		if len(items) > 2048 {
			return nil, templateError("模板包含过多出站或端点")
		}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, templateError("出站和端点必须使用 JSON 对象")
			}
			tag, ok := object["tag"].(string)
			kind, _ := object["type"].(string)
			if !ok || tag == "" || kind == "" || tags[tag] {
				return nil, templateError("出站和端点需要非空 type 和唯一 tag")
			}
			tags[tag] = true
		}
	}
	return tags, nil
}

func assembleSingBoxTemplate(config map[string]any, nodes []singBoxOutbound) ([]byte, error) {
	tags, err := templateTags(config)
	if err != nil {
		return nil, err
	}
	nodeTags := make([]string, len(nodes))
	for i, node := range nodes {
		tags[node.Tag], nodeTags[i] = true, node.Tag
	}
	outbounds, _ := config["outbounds"].([]any)
	emptyDirect := map[string]bool{}
	for _, item := range outbounds {
		outbound := item.(map[string]any)
		if outbound["type"] == "direct" && len(outbound) == 2 {
			emptyDirect[outbound["tag"].(string)] = true
		}
	}
	if dns, ok := config["dns"].(map[string]any); ok {
		servers, _ := dns["servers"].([]any)
		for _, item := range servers {
			server, _ := item.(map[string]any)
			detour, _ := server["detour"].(string)
			if emptyDirect[detour] {
				return nil, templateError("DNS 不能 detour 到空 direct 出站；默认直连时请省略 detour")
			}
		}
	}
	dependencies := map[string][]string{}
	for _, item := range outbounds {
		outbound := item.(map[string]any)
		kind, tag := outbound["type"].(string), outbound["tag"].(string)
		if kind != "selector" && kind != "urltest" {
			continue
		}
		members, exists := outbound["outbounds"]
		list, ok := members.([]any)
		if exists && !ok {
			return nil, templateError("节点选择组的 outbounds 必须是字符串数组")
		}
		if len(list) == 0 {
			list = []any{singBoxNodesPlaceholder}
		}
		expanded, seen := []string{}, map[string]bool{}
		for _, member := range list {
			name, ok := member.(string)
			if !ok {
				return nil, templateError("节点选择组只接受节点或分组标签")
			}
			values := []string{name}
			if name == singBoxNodesPlaceholder {
				values = nodeTags
			}
			for _, value := range values {
				if !tags[value] || value == tag {
					return nil, templateError("节点选择组包含不存在的标签或引用自身")
				}
				if !seen[value] {
					expanded = append(expanded, value)
					seen[value] = true
				}
			}
		}
		if selected, exists := outbound["default"]; exists {
			name, ok := selected.(string)
			if !ok || !seen[name] {
				return nil, templateError("选择组 default 必须是该组的成员")
			}
		}
		outbound["outbounds"] = expanded
		dependencies[tag] = expanded
	}
	if route, ok := config["route"].(map[string]any); ok {
		if final, exists := route["final"]; exists && final != "" {
			name, ok := final.(string)
			if !ok || !tags[name] || name == singBoxNodesPlaceholder {
				return nil, templateError("route.final 必须引用已存在的出站或端点")
			}
		}
	}
	visited, active := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(tag string) bool {
		if active[tag] {
			return false
		}
		if visited[tag] {
			return true
		}
		active[tag] = true
		for _, member := range dependencies[tag] {
			if !visit(member) {
				return false
			}
		}
		active[tag], visited[tag] = false, true
		return true
	}
	for tag := range dependencies {
		if !visit(tag) {
			return nil, templateError("节点选择组之间存在循环引用")
		}
	}
	for _, node := range nodes {
		outbounds = append(outbounds, node)
	}
	config["outbounds"] = outbounds
	return json.MarshalIndent(config, "", "  ")
}

func (s *Store) loadSingBoxTemplateLocked(options SingBoxOptions) ([]byte, error) {
	var record model.SingBoxTemplate
	if err := s.db.First(&record, subscriptionRecordID).Error; err == nil {
		if len(record.Content) > maxSingBoxTemplateBytes {
			return nil, templateError("数据库模板超过 512 KiB")
		}
		return []byte(record.Content), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load sing-box template: %w", err)
	}
	raw, err := DefaultSingBoxTemplate(options)
	if err != nil {
		return nil, err
	}
	record = model.SingBoxTemplate{ID: subscriptionRecordID, Content: string(raw)}
	// A concurrent upgrade or save must never be overwritten by preset initialization.
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
		return nil, fmt.Errorf("initialize sing-box template: %w", err)
	}
	if err := s.db.First(&record, subscriptionRecordID).Error; err != nil {
		return nil, fmt.Errorf("reload sing-box template: %w", err)
	}
	return []byte(record.Content), nil
}

func (s *Store) SingBoxTemplate() ([]byte, error) {
	s.singBoxMu.Lock()
	defer s.singBoxMu.Unlock()
	return s.loadSingBoxTemplateLocked(currentSingBoxOptions())
}

func (s *Store) SaveSingBoxTemplate(raw []byte) error {
	config, err := parseSingBoxTemplate(raw)
	if err != nil {
		return err
	}
	formatted, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if len(formatted) > maxSingBoxTemplateBytes {
		return templateError("格式化后的模板超过 512 KiB")
	}
	s.singBoxMu.Lock()
	defer s.singBoxMu.Unlock()
	nodes, err := s.Nodes()
	if err != nil {
		return err
	}
	_, report := BuildSingBoxConfigWithTemplate(nodes, SingBoxOptions{AllowPartial: true}, formatted)
	if report.Converted == 0 {
		if _, err := assembleSingBoxTemplate(config, nil); err != nil {
			return err
		}
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "INVALID_TEMPLATE" {
			return templateError(diagnostic.Message)
		}
	}
	record := model.SingBoxTemplate{ID: subscriptionRecordID, Content: string(formatted)}
	return s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"})}).Create(&record).Error
}

func singBoxTemplateHash(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }
