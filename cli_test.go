package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseClientCommandUsesReadableDefaults(t *testing.T) {
	cmd, err := parseCommand([]string{"client", "-server", "example.com:34197"})
	if err != nil {
		t.Fatal(err)
	}

	client, ok := cmd.(clientCommand)
	if !ok {
		t.Fatalf("command type = %T, want clientCommand", cmd)
	}
	if client.udpListen != "127.0.0.1:34197" {
		t.Fatalf("udp listen = %q, want default localhost Factorio port", client.udpListen)
	}
	if client.serverAddr != "example.com:34197" {
		t.Fatalf("server addr = %q", client.serverAddr)
	}
}

func TestParseServerCommandRequiresUDPRemote(t *testing.T) {
	_, err := parseCommand([]string{"server"})
	if err == nil {
		t.Fatal("parseCommand(server without -udp) succeeded")
	}
}

func TestParseCommandLoadsServerConfigFromCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeConfig(t, "server.json", `{"listen":"127.0.0.1:5000","udp":"127.0.0.1:6000"}`)

	cmd, err := parseCommand(nil)
	if err != nil {
		t.Fatal(err)
	}

	server, ok := cmd.(serverCommand)
	if !ok {
		t.Fatalf("command type = %T, want serverCommand", cmd)
	}
	if server.tcpListen != "127.0.0.1:5000" {
		t.Fatalf("tcp listen = %q", server.tcpListen)
	}
	if server.udpRemote != "127.0.0.1:6000" {
		t.Fatalf("udp remote = %q", server.udpRemote)
	}
}

func TestParseCommandLoadsClientConfigFromCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeConfig(t, "client.json", `{"listen":"127.0.0.1:7000","server":"example.com:8000"}`)

	cmd, err := parseCommand(nil)
	if err != nil {
		t.Fatal(err)
	}

	client, ok := cmd.(clientCommand)
	if !ok {
		t.Fatalf("command type = %T, want clientCommand", cmd)
	}
	if client.udpListen != "127.0.0.1:7000" {
		t.Fatalf("udp listen = %q", client.udpListen)
	}
	if client.serverAddr != "example.com:8000" {
		t.Fatalf("server addr = %q", client.serverAddr)
	}
}

func TestParseCommandRejectsAmbiguousConfigDiscovery(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeConfig(t, "server.json", `{"udp":"127.0.0.1:6000"}`)
	writeConfig(t, "client.json", `{"server":"example.com:8000"}`)

	_, err := parseCommand(nil)
	if err == nil {
		t.Fatal("parseCommand with both server.json and client.json succeeded")
	}
	if !strings.Contains(err.Error(), "both server.json and client.json") {
		t.Fatalf("error = %q, want ambiguity message", err)
	}
}

func TestParseCommandLoadsPositionalServerConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/server.json"
	writeConfig(t, path, `{"listen":"127.0.0.1:5000","udp":"127.0.0.1:6000"}`)

	cmd, err := parseCommand([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	server := cmd.(serverCommand)
	if server.tcpListen != "127.0.0.1:5000" {
		t.Fatalf("tcp listen = %q", server.tcpListen)
	}
	if server.udpRemote != "127.0.0.1:6000" {
		t.Fatalf("udp remote = %q", server.udpRemote)
	}
}

func TestParseCommandLoadsPositionalClientConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/client.json"
	writeConfig(t, path, `{"listen":"127.0.0.1:7000","server":"example.com:8000"}`)

	cmd, err := parseCommand([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	client := cmd.(clientCommand)
	if client.udpListen != "127.0.0.1:7000" {
		t.Fatalf("udp listen = %q", client.udpListen)
	}
	if client.serverAddr != "example.com:8000" {
		t.Fatalf("server addr = %q", client.serverAddr)
	}
}

func TestParseServerCommandConfigFlagAndOverrides(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/server.json"
	writeConfig(t, path, `{"listen":"127.0.0.1:5000","udp":"127.0.0.1:6000"}`)

	cmd, err := parseCommand([]string{"server", "-config", path, "-listen", "127.0.0.1:9000"})
	if err != nil {
		t.Fatal(err)
	}

	server := cmd.(serverCommand)
	if server.tcpListen != "127.0.0.1:9000" {
		t.Fatalf("tcp listen = %q", server.tcpListen)
	}
	if server.udpRemote != "127.0.0.1:6000" {
		t.Fatalf("udp remote = %q", server.udpRemote)
	}
}

func TestParseClientCommandConfigFlagAndOverrides(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/client.json"
	writeConfig(t, path, `{"listen":"127.0.0.1:7000","server":"example.com:8000"}`)

	cmd, err := parseCommand([]string{"client", "-config", path, "-server", "override.example:9000"})
	if err != nil {
		t.Fatal(err)
	}

	client := cmd.(clientCommand)
	if client.udpListen != "127.0.0.1:7000" {
		t.Fatalf("udp listen = %q", client.udpListen)
	}
	if client.serverAddr != "override.example:9000" {
		t.Fatalf("server addr = %q", client.serverAddr)
	}
}

func TestParseCommandRejectsUnknownJSONFields(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/server.json"
	writeConfig(t, path, `{"udp":"127.0.0.1:6000","extra":true}`)

	_, err := parseCommand([]string{"server", "-config", path})
	if err == nil {
		t.Fatal("parseCommand with unknown JSON field succeeded")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %q, want unknown field message", err)
	}
}

func TestParseCommandRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/client.json"
	writeConfig(t, path, `{"server":`)

	_, err := parseCommand([]string{"client", "-config", path})
	if err == nil {
		t.Fatal("parseCommand with malformed JSON succeeded")
	}
}

func TestParseCommandRejectsExtraArgsAfterPositionalConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/client.json"
	writeConfig(t, path, `{"server":"example.com:34197"}`)

	_, err := parseCommand([]string{path, "extra"})
	if err == nil {
		t.Fatal("parseCommand with extra positional args succeeded")
	}
	if !strings.Contains(err.Error(), "unexpected arguments after") {
		t.Fatalf("error = %q, want extra args message", err)
	}
}

func TestCommandConfigsUseHumanReadableLogger(t *testing.T) {
	cmd, err := parseCommand([]string{"server", "-udp", "127.0.0.1:34197"})
	if err != nil {
		t.Fatal(err)
	}

	server := cmd.(serverCommand)
	cfg := server.config(nil)
	if cfg.Log == nil {
		t.Fatal("server logger is nil")
	}

	client := clientCommand{udpListen: "127.0.0.1:34197", serverAddr: "example.com:34197"}
	clientCfg := client.config(nil)
	if clientCfg.Log == nil {
		t.Fatal("client logger is nil")
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
