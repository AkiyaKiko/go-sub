package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestBootstrapCredentialsAreIndependentAndNeverLogged(t *testing.T) {
	config := NewDefaultConfig()
	suffix := strings.TrimPrefix(config.Settings.Admin, "admin_")
	if len(config.Settings.Password) < 32 || strings.Contains(config.Settings.Password, suffix) {
		t.Fatal("password can be derived from the administrator name")
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	printStartupInfo(config.Settings, "/private-startup-token", true)
	if strings.Contains(output.String(), config.Settings.Password) || strings.Contains(output.String(), "private-startup-token") {
		t.Fatal("startup logs exposed credentials")
	}
}
