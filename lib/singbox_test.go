package lib

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"git.local/gosub/model"
)

const testVLESSUUID = "11111111-1111-4111-8111-111111111111"

func testVLESS(query string) string {
	return "vless://" + testVLESSUUID + "@127.0.0.1:443?encryption=none&" + query
}

func singBoxFixtures() []string {
	publicKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	ssKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 16))
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:p:@+/=%#word"))
	ssLegacy := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:legacy:@password@[2001:db8::1]:8388"))
	fixtures := []string{
		testVLESS("security=none&type=tcp#plain"),
		testVLESS("security=tls&type=tcp&sni=example.com&alpn=h2%2Chttp%2F1.1&fp=chrome#tls"),
		testVLESS("security=reality&type=tcp&flow=xtls-rprx-vision&sni=example.com&fp=chrome&pbk=" + publicKey + "&sid=0123456789abcdef#reality"),
		testVLESS("security=tls&type=ws&host=cdn.example.com&path=%2Fsocket%253Fraw#ws"),
		testVLESS("security=tls&type=ws&path=%2Fsocket%3Fed%3D2048#ws-early"),
		testVLESS("security=tls&type=grpc&serviceName=rpc%2Fservice&mode=gun#grpc"),
		testVLESS("security=tls&type=http&host=one.example%2Ctwo.example&path=%2Fproxy#http"),
		testVLESS("security=tls&type=httpupgrade&host=cdn.example&path=%2Fupgrade#upgrade"),
		testVLESS("security=tls&type=quic#quic"),
		testVLESS("security=none&type=tcp&packetEncoding=none#no-packet-encoding"),
		"ss://" + ssUser + "@127.0.0.1:8388#SS-%E4%B8%AD%E6%96%87",
		"ss://aes-256-gcm:p%3A%40%2B%2F%3D%25%23word@127.0.0.1:8388#plain-ss",
		"ss://" + ssLegacy + "#legacy-ss",
		"ss://2022-blake3-aes-128-gcm:" + url.PathEscape(ssKey) + "@127.0.0.1:8388#ss2022",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:password")) + "@127.0.0.1:8388/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#obfs",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:password")) + "@127.0.0.1:8388/?plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Bhost%3Dexample.com%3Bpath%3D%2Fsocket%3Btls#plugin",
		"ss://none:@127.0.0.1:8388#unencrypted-ss",
		testVLESS("security=none&type=ws&path=%2Fsocket#plain-ws"),
		testVLESS("security=none&type=grpc&serviceName=rpc#plain-grpc"),
		testVLESS("security=none&type=http&path=%2Fproxy#plain-http"),
		testVLESS("security=none&type=httpupgrade&path=%2Fupgrade#plain-upgrade"),
		testVLESS("security=reality&type=tcp&flow=xtls-rprx-vision&sni=example.com&pbk=" + publicKey + "#reality-default-fingerprint"),
	}
	return append(fixtures, expandedSingBoxFixtures()...)
}

func testVMess(extra map[string]any) string {
	fields := map[string]any{"v": "2", "add": "127.0.0.1", "port": "443", "id": testVLESSUUID, "aid": "0", "net": "tcp", "type": "none", "tls": ""}
	for key, value := range extra {
		fields[key] = value
	}
	data, _ := json.Marshal(fields)
	return "vmess://" + base64.RawURLEncoding.EncodeToString(data)
}

func expandedSingBoxFixtures() []string {
	return []string{
		testVMess(map[string]any{"ps": "vmess-tcp", "aid": 64, "port": 443, "scy": "aes-128-gcm"}),
		testVMess(map[string]any{"ps": "vmess-ws", "net": "ws", "host": "cdn.example.com", "path": "/socket%2Fraw?ed=1024", "tls": "tls", "sni": "example.com", "alpn": "http/1.1", "fp": "chrome", "insecure": false}),
		testVMess(map[string]any{"ps": "vmess-grpc", "net": "grpc", "path": "rpc/service", "type": "gun", "tls": true}),
		testVMess(map[string]any{"ps": "vmess-h2", "net": "h2", "host": "cdn.example.com", "path": "/h2", "tls": "tls"}),
		"vmess://" + testVLESSUUID + "@127.0.0.1:443?encryption=chacha20-poly1305&security=tls&type=ws&path=%2Fsocket&alterId=0#vmess-url",
		"trojan://p%3A%40%2B%2F%3D%25%23word@127.0.0.1#trojan-tcp",
		"trojan://password@127.0.0.1:443?type=ws&path=%2Fsocket&host=cdn.example.com&peer=example.com&allowInsecure=0#trojan-ws",
		"trojan://password@127.0.0.1:443?type=grpc&serviceName=rpc%2Fservice#trojan-grpc",
		"hy://127.0.0.1:443?auth=p%3A%40%252Fword&upmbps=20&downmbps=100&obfs=xplus&obfsParam=obfs-secret&peer=example.com&alpn=h3#hy1",
		"hysteria2://user:pass%3A%40%252F@127.0.0.1:443/?obfs=salamander&obfs-password=obfs-secret&sni=example.com&upmbps=20&downmbps=100#hy2-auth",
		"hy2://127.0.0.1#hy2-default",
		"hy2://password@[2001:db8::1]:443,5000-5002/?hop_interval=10&insecure=0#hy2-hopping",
		"hy2://password@127.0.0.1?mport=5000-5002%3B5005&hop-interval=15s#hy2-mport",
		"hy2://password@127.0.0.1?obfs=gecko&obfs-password=obfs-secret#hy2-gecko",
		"tuic://" + testVLESSUUID + ":p%3A%40%252F@127.0.0.1?congestion_control=bbr&udp_relay_mode=quic&disable_sni=1&reduce_rtt=1&alpn=h3#tuic-quic",
		"tuic://" + testVLESSUUID + ":password@127.0.0.1:443?udp-over-stream=true&heartbeat=5s#tuic-stream",
		"anytls://p%3A%40%252F@127.0.0.1?sni=example.com&insecure=0#anytls-default",
		"anytls://password@127.0.0.1:443?fp=chrome&idle-session-check-interval=15s&idle-session-timeout=30&min-idle-session=3#anytls-idle",
		"http://user:p%3A%40%252F@127.0.0.1#http-proxy",
		"https://user:password@[2001:db8::1]:8443?sni=example.com&skip-cert-verify=1#https-proxy",
		"socks5://user:p%3A%40%252F@127.0.0.1#socks5-proxy",
		"socks://127.0.0.1:1081#socks-proxy",
		testVMess(map[string]any{"ps": "vmess-verify-alias", "tls": "tls", "allowInsecure": "1", "verify_cert": false}),
		"hy2://password@127.0.0.1?insecure=true&skip-cert-verify=1#hy2-verify-alias",
		testVMess(map[string]any{"ps": "vmess-plain-defaults", "insecure": "0", "vcn": "", "pcs": "", "host": "", "path": "", "sni": "", "alpn": "", "fp": ""}),
		"hysteria://127.0.0.1:443,5000-5002?auth=password&upmbps=20&downmbps=100&hop-interval=5#hy1-hopping",
	}
}

func TestSingBoxExpandedProtocolParameters(t *testing.T) {
	body, report := BuildSingBoxConfig(expandedSingBoxFixtures(), SingBoxOptions{})
	if report.Failed != 0 || report.Status != "ready" {
		t.Fatal(report)
	}
	var config struct {
		Outbounds []singBoxOutbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	byTag := map[string]singBoxOutbound{}
	for _, outbound := range config.Outbounds {
		byTag[outbound.Tag] = outbound
	}
	if node := byTag["vmess-tcp"]; node.Type != "vmess" || node.Security != "aes-128-gcm" || node.AlterID != 64 || node.UUID != testVLESSUUID || node.TLS != nil {
		t.Fatalf("VMess parameters lost: %+v", node)
	}
	if node := byTag["vmess-ws"]; node.Transport == nil || node.Transport.Path != "/socket%2Fraw" || node.Transport.MaxEarlyData != 1024 || node.TLS == nil || node.TLS.ServerName != "example.com" || node.TLS.UTLS.Fingerprint != "chrome" {
		t.Fatalf("VMess WS/TLS changed: %+v", node)
	}
	if node := byTag["vmess-grpc"]; node.Transport == nil || node.Transport.Type != "grpc" || node.Transport.ServiceName != "rpc/service" || node.TLS == nil {
		t.Fatalf("VMess gRPC parameters lost: %+v", node)
	}
	if node := byTag["trojan-tcp"]; node.Type != "trojan" || node.Password != "p:@+/=%#word" || node.ServerPort != 443 || node.TLS == nil || node.TLS.Insecure {
		t.Fatalf("Trojan credentials or TLS changed: %+v", node)
	}
	if node := byTag["trojan-ws"]; node.TLS.ServerName != "example.com" || node.Transport.Headers["Host"] != "cdn.example.com" {
		t.Fatal("Trojan peer or WebSocket host was lost")
	}
	if node := byTag["hy1"]; node.Type != "hysteria" || node.AuthString != "p:@%2Fword" || node.Obfs != "obfs-secret" || node.UpMbps != 20 || node.DownMbps != 100 || node.Password != "" {
		t.Fatalf("Hysteria 1 parameters lost: %+v", node)
	}
	if node := byTag["hy2-auth"]; node.Type != "hysteria2" || node.Password != "user:pass:@%2F" || node.UpMbps != 20 || node.DownMbps != 100 || node.Obfs == nil {
		t.Fatalf("Hysteria 2 authentication changed: %+v", node)
	}
	if node := byTag["hy2-hopping"]; node.Server != "2001:db8::1" || node.ServerPort != 0 || !reflect.DeepEqual(node.ServerPorts, []string{"443:443", "5000:5002"}) || node.HopInterval != "10s" {
		t.Fatalf("Hysteria 2 port hopping lost: %+v", node)
	}
	if node := byTag["tuic-quic"]; node.Type != "tuic" || node.UUID != testVLESSUUID || node.Password != "p:@%2F" || node.Username != "" || node.CongestionControl != "bbr" || node.UDPRelayMode != "quic" || !node.ZeroRTTHandshake || !node.TLS.DisableSNI {
		t.Fatalf("TUIC v5 parameters lost: %+v", node)
	}
	if node := byTag["tuic-stream"]; !node.UDPOverStream || node.UDPRelayMode != "" || node.Heartbeat != "5s" {
		t.Fatal("TUIC stream relay changed")
	}
	if node := byTag["anytls-default"]; node.Type != "anytls" || node.Password != "p:@%2F" || node.ServerPort != 443 || node.TLS == nil || node.TLS.Insecure {
		t.Fatal("AnyTLS credential, default port or certificate verification changed")
	}
	if node := byTag["anytls-idle"]; node.IdleSessionCheckInterval != "15s" || node.IdleSessionTimeout != "30s" || node.MinIdleSession != 3 {
		t.Fatal("AnyTLS session parameters lost")
	}
	for _, tag := range []string{"http-proxy", "socks5-proxy"} {
		if node := byTag[tag]; node.Username != "user" || node.Password != "p:@%2F" || node.TLS != nil {
			t.Fatalf("proxy credentials changed for %s", tag)
		}
	}
	if node := byTag["https-proxy"]; node.Type != "http" || node.Server != "2001:db8::1" || node.TLS == nil || !node.TLS.Insecure || node.TLS.ServerName != "example.com" {
		t.Fatal("HTTPS proxy TLS changed")
	}
	if node := byTag["socks5-proxy"]; node.Type != "socks" || node.Version != "5" || node.ServerPort != 1080 {
		t.Fatal("SOCKS5 version or default port changed")
	}
}

func TestSingBoxSupportedShareFormats(t *testing.T) {
	fixtures := singBoxFixtures()
	body, report := BuildSingBoxConfig(fixtures, SingBoxOptions{})
	if report.Status != "ready" || report.Converted != len(fixtures) || report.Failed != 0 {
		t.Fatalf("conversion report = %+v", report)
	}
	var config struct {
		Outbounds []singBoxOutbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Outbounds) != len(fixtures)+3 {
		t.Fatalf("outbound count = %d", len(config.Outbounds))
	}
	ws := config.Outbounds[6]
	if ws.Transport.Type != "ws" || ws.Transport.Headers["Host"] != "cdn.example.com" || ws.Transport.Path != "/socket%3Fraw" {
		t.Fatalf("WebSocket parameters were changed: %+v", ws.Transport)
	}
	for _, index := range []int{13, 14} {
		if config.Outbounds[index].Password != "p:@+/=%#word" {
			t.Fatalf("SS password changed at %d", index)
		}
	}
	if config.Outbounds[15].Server != "2001:db8::1" || config.Outbounds[15].Password != "legacy:@password" {
		t.Fatal("legacy Shadowsocks IPv6 or password changed")
	}
	if config.Outbounds[12].PacketEncoding == nil || *config.Outbounds[12].PacketEncoding != "" {
		t.Fatal("explicit disabled packet encoding was lost")
	}
	var all map[string]any
	if err := json.Unmarshal(body, &all); err != nil {
		t.Fatal(err)
	}
	if all["inbounds"] == nil || all["route"] == nil || all["experimental"] == nil {
		t.Fatal("export is not a complete mixed profile")
	}
}

func TestSingBoxRejectsLossyConversionsAndRedactsDiagnostics(t *testing.T) {
	secret := "do-not-leak-this-password"
	tests := []struct{ uri, code string }{
		{testVLESS("security=none&type=xhttp"), "UNSUPPORTED_TRANSPORT_XHTTP"},
		{testVLESS("security=none&type=kcp"), "UNSUPPORTED_TRANSPORT"},
		{testVLESS("security=none&type=quic"), "MISSING_QUIC_TLS"},
		{testVLESS("security=tls&type=quic&fp=chrome"), "UNSUPPORTED_TLS_TRANSPORT"},
		{testVLESS("security=none&type=tcp&headerType=http"), "UNSUPPORTED_HEADER_TYPE"},
		{testVLESS("security=tls&type=grpc&mode=multi"), "UNSUPPORTED_GRPC_MODE"},
		{testVLESS("security=tls&type=grpc&authority=example.com"), "UNSUPPORTED_GRPC_MODE"},
		{testVLESS("security=tls&type=tcp&fp=not-a-fingerprint"), "UNSUPPORTED_TLS_FINGERPRINT"},
		{testVLESS("security=reality&type=tcp&sni=example.com&pbk=" + secret), "INVALID_REALITY_PUBLIC_KEY"},
		{testVLESS("security=none&type=tcp&encryption=" + secret), "INVALID_QUERY"},
		{testVLESS("security=none&type=tcp&unknown=" + secret), "UNSUPPORTED_PARAMETER"},
		{testVLESS("security=tls&type=tcp&allowInsecure=maybe"), "INVALID_TLS"},
		{testVLESS("security=none&type=tcp&flow=xtls-rprx-vision"), "UNSUPPORTED_FLOW_TRANSPORT"},
		{testVLESS("security=none&insecure=1"), "INVALID_TLS"},
		{"vless://not-a-uuid@127.0.0.1:443", "INVALID_UUID"},
		{"vless://" + testVLESSUUID + "@127.0.0.1:0", "INVALID_ENDPOINT"},
		{"ss://not-base64@127.0.0.1:8388", "INVALID_BASE64"},
		{"ss://unknown:" + secret + "@127.0.0.1:8388", "UNSUPPORTED_SS_METHOD"},
		{"ss://aes-128-gcm:" + secret + "@127.0.0.1:8388?plugin=unknown", "UNSUPPORTED_SS_PLUGIN"},
		{"ss://2022-blake3-aes-128-gcm:" + secret + "@127.0.0.1:8388", "INVALID_SS2022_KEY"},
		{"ssr://" + secret, "UNSUPPORTED_SCHEME"},
		{"trojan://" + secret + "@127.0.0.1:443?type=xhttp", "UNSUPPORTED_TRANSPORT_XHTTP"},
		{"trojan://@127.0.0.1:443", "MISSING_PASSWORD"},
		{"anytls://" + secret + "@127.0.0.1?security=none", "UNSUPPORTED_SECURITY"},
		{"anytls://" + secret + "@127.0.0.1?type=ws", "UNSUPPORTED_PARAMETER"},
		{"hy2://" + secret + "@127.0.0.1?obfs=salamander", "INVALID_OBFS"},
		{"hy2://" + secret + "@127.0.0.1?obfs=none&obfs-password=" + secret, "INVALID_OBFS"},
		{"hy2://" + secret + "@127.0.0.1?fp=chrome", "UNSUPPORTED_TLS_TRANSPORT"},
		{"hy2://" + secret + "@127.0.0.1?pinSHA256=" + secret, "UNSUPPORTED_PARAMETER"},
		{"hy2://" + secret + "@127.0.0.1?insecure=maybe", "INVALID_TLS"},
		{"hy2://" + secret + "@127.0.0.1?peer=one.example&sni=two.example", "INVALID_QUERY"},
		{"hy2://" + secret + "@127.0.0.1?insecure=0&allow-insecure=1", "INVALID_TLS"},
		{"hy2://" + secret + "@127.0.0.1:5002-5000", "INVALID_PORT_RANGE"},
		{"hy2://" + secret + "@127.0.0.1?mport=", "INVALID_PORT_RANGE"},
		{"hy2://" + secret + "@127.0.0.1?mport=443,,444", "INVALID_PORT_RANGE"},
		{"hy2://" + secret + "@127.0.0.1:443-445?mport=450-460", "INVALID_QUERY"},
		{"hy2://" + secret + "@127.0.0.1?hop-interval=10", "INVALID_QUERY"},
		{"hy2://" + secret + "@127.0.0.1?upmbps=-1", "INVALID_NUMBER"},
		{"hy://127.0.0.1?auth=" + secret, "MISSING_BANDWIDTH"},
		{"hy://127.0.0.1?upmbps=20&downmbps=100&protocol=faketcp", "UNSUPPORTED_HYSTERIA_PROTOCOL"},
		{"hy://127.0.0.1?upmbps=20&downmbps=100&obfs=xplus", "INVALID_OBFS"},
		{"hy://127.0.0.1?upmbps=20&downmbps=100&obfs-password=" + secret, "UNSUPPORTED_PARAMETER"},
		{"tuic://token@127.0.0.1", "INVALID_UUID"},
		{"tuic://" + testVLESSUUID + "@127.0.0.1", "INVALID_CREDENTIALS"},
		{"tuic://" + testVLESSUUID + ":" + secret + "@127.0.0.1?congestion-control=unknown", "UNSUPPORTED_CONGESTION_CONTROL"},
		{"tuic://" + testVLESSUUID + ":" + secret + "@127.0.0.1?udp-relay-mode=quic&udp-over-stream=1", "INVALID_QUERY"},
		{"tuic://" + testVLESSUUID + ":" + secret + "@127.0.0.1?reduce-rtt=maybe", "INVALID_BOOLEAN"},
		{"tuic://" + testVLESSUUID + ":" + secret + "@127.0.0.1?heartbeat=-1s", "INVALID_DURATION"},
		{"socks5://user:" + secret + "@127.0.0.1?security=tls", "UNSUPPORTED_SECURITY"},
		{"socks5+tls://user:" + secret + "@127.0.0.1", "UNSUPPORTED_SCHEME"},
		{"https://user:" + secret + "@127.0.0.1/resource", "INVALID_URI"},
		{"https://user:" + secret + "@127.0.0.1?security=none", "UNSUPPORTED_SECURITY"},
		{testVMess(map[string]any{"net": "xhttp"}), "UNSUPPORTED_TRANSPORT_XHTTP"},
		{testVMess(map[string]any{"net": "tcp", "type": "http"}), "UNSUPPORTED_HEADER_TYPE"},
		{testVMess(map[string]any{"net": "grpc", "type": "multi"}), "UNSUPPORTED_GRPC_MODE"},
		{testVMess(map[string]any{"net": "h2"}), "INVALID_TLS"},
		{testVMess(map[string]any{"scy": secret}), "UNSUPPORTED_VMESS_SECURITY"},
		{testVMess(map[string]any{"aid": "0.5"}), "INVALID_NUMBER"},
		{testVMess(map[string]any{"extra": secret}), "UNSUPPORTED_PARAMETER"},
		{testVMess(map[string]any{"pcs": secret}), "UNSUPPORTED_PARAMETER"},
		{testVMess(map[string]any{"vcn": secret}), "UNSUPPORTED_PARAMETER"},
		{testVMess(map[string]any{"tls": "reality"}), "UNSUPPORTED_SECURITY"},
		{testVMess(map[string]any{"tls": "tls", "insecure": true, "verify_cert": true}), "INVALID_TLS"},
		{"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"id":"`+secret+`","id":"duplicate"}`)), "INVALID_VMESS_JSON"},
		{"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"id":null}`)), "INVALID_VMESS_JSON"},
		{"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{} {}`)), "INVALID_VMESS_JSON"},
	}
	for index, tt := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			body, report := BuildSingBoxConfig([]string{tt.uri}, SingBoxOptions{})
			if len(body) != 0 || report.Failed != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != tt.code {
				t.Fatalf("report = %+v", report)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), secret) || strings.Contains(string(data), testVLESSUUID) || strings.Contains(string(data), "://") {
				t.Fatal("diagnostic exposed a credential or source URI")
			}
		})
	}
}

func TestSingBoxTagsAndStrictPolicy(t *testing.T) {
	nodes := []string{testVLESS("type=tcp#proxy"), testVLESS("type=tcp#%E9%A6%99%E6%B8%AF"), testVLESS("type=tcp#%E9%A6%99%E6%B8%AF")}
	body, report := BuildSingBoxConfig(nodes, SingBoxOptions{})
	if report.Converted != 3 {
		t.Fatal(report)
	}
	secondBody, _ := BuildSingBoxConfig(nodes, SingBoxOptions{})
	if !bytes.Equal(body, secondBody) {
		t.Fatal("unchanged source produced different JSON")
	}
	var config struct {
		Outbounds []singBoxOutbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, outbound := range config.Outbounds {
		if seen[outbound.Tag] {
			t.Fatalf("duplicate tag %q", outbound.Tag)
		}
		seen[outbound.Tag] = true
	}
	for _, outbound := range config.Outbounds {
		for _, tag := range outbound.Outbounds {
			if !seen[tag] {
				t.Fatal("group references missing tag")
			}
		}
	}
	nodes = append(nodes, "vmess://unsupported")
	if body, report = BuildSingBoxConfig(nodes, SingBoxOptions{}); len(body) != 0 || report.Failed != 1 {
		t.Fatal("strict conversion published partial output")
	}
	if body, report = BuildSingBoxConfig(nodes, SingBoxOptions{AllowPartial: true}); len(body) == 0 || report.Converted != 3 || report.Status != "warning" {
		t.Fatal("explicit partial conversion did not publish compatible nodes")
	}
	if body, _ = BuildSingBoxConfig([]string{"vmess://unsupported"}, SingBoxOptions{AllowPartial: true}); len(body) != 0 {
		t.Fatal("all-invalid input produced a profile")
	}
}

func TestSingBoxInputAndTemplateOptions(t *testing.T) {
	body, report := BuildSingBoxConfig([]string{" \ufeff" + testVLESS("type=tcp#name") + "\r\n"}, SingBoxOptions{MixedPort: 1080, DisableClashAPI: true})
	if len(body) == 0 || report.Failed != 0 || strings.Contains(string(body), "clash_api") || !strings.Contains(string(body), `"listen_port": 1080`) {
		t.Fatal("BOM/CRLF or template options not handled")
	}
	if body, _ := BuildSingBoxConfig([]string{strings.Repeat("x", maxShareURIBytes+1)}, SingBoxOptions{}); len(body) != 0 {
		t.Fatal("overlong URI accepted")
	}
	manyNodes := make([]string, maxSingBoxNodes+1)
	for i := range manyNodes {
		manyNodes[i] = testVLESS("type=tcp")
	}
	if body, _ := BuildSingBoxConfig(manyNodes, SingBoxOptions{}); len(body) != 0 {
		t.Fatal("too many nodes accepted")
	}
	if body, report := BuildSingBoxConfig([]string{"", " \ufeff\r\n "}, SingBoxOptions{MixedPort: -1}); len(body) != 0 || report.Status != "disabled" {
		t.Fatal("empty source retained an invalid template instead of disabling publication")
	}
	for _, options := range []SingBoxOptions{{MixedPort: -1}, {MixedPort: 9090}, {TestInterval: "1s"}, {TestURL: "file:///secret"}} {
		if body, report := BuildSingBoxConfig([]string{testVLESS("type=tcp")}, options); len(body) != 0 || report.Diagnostics[0].Code != "INVALID_TEMPLATE" {
			t.Fatal("invalid template accepted")
		}
	}
}

func TestSingBoxPublicationPreservesGoodVersionAndClearsOnEmpty(t *testing.T) {
	store := newTestStore(t)
	valid := []string{testVLESS("type=tcp#valid")}
	if err := store.ReplaceNodes(valid); err != nil {
		t.Fatal(err)
	}
	first, report, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || len(first) == 0 || report.Status != "ready" {
		t.Fatalf("first publication: %+v, %v", report, err)
	}
	generatedAt := report.GeneratedAt
	if err := store.ReplaceNodes([]string{"vmess://unsupported"}); err != nil {
		t.Fatal("sing-box conversion blocked Xray save")
	}
	body, report, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || !bytes.Equal(first, body) || report.Status != "stale" || report.GeneratedAt != generatedAt || report.SourceHash == report.PublishedHash {
		t.Fatalf("last-good publication lost: %+v, %v", report, err)
	}
	if nodes, _ := store.Nodes(); !reflect.DeepEqual(nodes, []string{"vmess://unsupported"}) {
		t.Fatal("Xray nodes were rolled back")
	}
	if err := store.ReplaceNodes(nil); err != nil {
		t.Fatal(err)
	}
	if body, report, err = store.SingBoxSubscription(SingBoxOptions{}); err != nil || len(body) != 0 || report.Status != "disabled" {
		t.Fatal("clearing nodes retained old credentials")
	}
}

func TestSingBoxConcurrentPublicationAndPathConflicts(t *testing.T) {
	if SingBoxPath("/sub") == SingBoxPath("/sub/") {
		t.Fatal("different subscription tokens produced the same sing-box path")
	}
	store := newTestStore(t)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			if err := store.ReplaceNodes([]string{testVLESS(fmt.Sprintf("type=tcp#node-%d", index))}); err != nil {
				t.Error(err)
			}
			if _, _, err := store.SingBoxSubscription(SingBoxOptions{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	workers.Wait()
	nodes, _ := store.Nodes()
	expected, _ := BuildSingBoxConfig(nodes, SingBoxOptions{})
	body, _, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || !bytes.Equal(expected, body) {
		t.Fatal("stale source overwrote current publication")
	}
	if err := store.CreateRouteRule("conflict", "/sub/sing-box", `{}`); err == nil {
		t.Fatal("rule shadowed sing-box subscription")
	}
	if err := store.CreateRouteRule("other", "/next/sing-box", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSubscriptionPath("/next"); err == nil {
		t.Fatal("subscription shadowed existing rule")
	}
}

func TestSingBoxPublicationSurvivesRestartAndInvalidTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodes([]string{testVLESS("type=tcp")}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	want, _, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	body, report, err := store.SingBoxSubscription(SingBoxOptions{MixedPort: -1})
	if err != nil || report.Status != "stale" || !bytes.Equal(body, want) || report.Diagnostics[0].Code != "INVALID_TEMPLATE" {
		t.Fatalf("invalid template lost persisted publication: %+v, %v", report, err)
	}
}

func TestSingBoxUpgradeRetriesPreviouslyUnsupportedProtocols(t *testing.T) {
	store := newTestStore(t)
	nodes := []string{"trojan://password@127.0.0.1:443"}
	if err := store.ReplaceNodes(nodes); err != nil {
		t.Fatal(err)
	}
	options, err := NormalizeSingBoxOptions(SingBoxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oldSource, err := json.Marshal(struct {
		Nodes    []string
		Options  SingBoxOptions
		Template string
	}{nodes, options, "desktop-mixed-v1"})
	if err != nil {
		t.Fatal(err)
	}
	oldHash := fmt.Sprintf("%x", sha256.Sum256(oldSource))
	oldReport, _ := json.Marshal(ConversionReport{Status: "error", Total: 1, Failed: 1, SourceHash: oldHash})
	if err := store.db.Save(&model.SingBoxPublication{ID: subscriptionRecordID, SourceHash: oldHash, ReportJSON: string(oldReport)}).Error; err != nil {
		t.Fatal(err)
	}
	body, report, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || len(body) == 0 || report.Status != "ready" || report.Converted != 1 || report.SourceHash == oldHash {
		t.Fatalf("upgrade reused unsupported-protocol cache: %+v, %v", report, err)
	}
}

func FuzzParseShareNode(f *testing.F) {
	for _, raw := range singBoxFixtures() {
		f.Add(raw)
	}
	for _, raw := range []string{"", "vmess://", "vmess://e30", "hy2://password@[::1]:", "hy2://password@:443-445", "hy2://password@host?mport=", "tuic://@host", "anytls://%00@host", "hy://[::1]"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		node, _, err := parseShareNode(raw)
		if err == nil && (node.Protocol == "" || node.Server == "" || node.Port == 0 && len(node.ServerPorts) == 0) {
			t.Fatal("parser accepted a node without protocol or endpoint")
		}
	})
}

func TestSingBoxOfficialCheck(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		binary, _ = exec.LookPath("sing-box")
	}
	if binary == "" {
		t.Skip("set SINGBOX_BINARY to the official sing-box 1.14.3 binary")
	}
	fixtures := singBoxFixtures()
	body, report := BuildSingBoxConfig(fixtures, SingBoxOptions{})
	if report.Failed != 0 {
		t.Fatal(report)
	}
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "check", "-c", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sing-box check: %v\n%s", err, output)
	}
}
