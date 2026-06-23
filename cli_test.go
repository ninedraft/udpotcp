package main

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
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

func TestParseHealthcheckCommandUsesReadableDefaults(t *testing.T) {
	cmd, err := parseCommand([]string{"healthcheck"})
	if err != nil {
		t.Fatal(err)
	}

	healthcheck, ok := cmd.(healthcheckCommand)
	if !ok {
		t.Fatalf("command type = %T, want healthcheckCommand", cmd)
	}
	if healthcheck.serverAddr != "127.0.0.1:34197" {
		t.Fatalf("server addr = %q, want default localhost server port", healthcheck.serverAddr)
	}
	if healthcheck.timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", healthcheck.timeout)
	}
}

func TestParseHealthcheckCommandFlags(t *testing.T) {
	cmd, err := parseCommand([]string{"healthcheck", "-server", "example.com:34197", "-timeout", "250ms"})
	if err != nil {
		t.Fatal(err)
	}

	healthcheck := cmd.(healthcheckCommand)
	if healthcheck.serverAddr != "example.com:34197" {
		t.Fatalf("server addr = %q", healthcheck.serverAddr)
	}
	if healthcheck.timeout != 250*time.Millisecond {
		t.Fatalf("timeout = %s, want 250ms", healthcheck.timeout)
	}
}

func TestParseHealthcheckCommandRejectsUnexpectedArgs(t *testing.T) {
	_, err := parseCommand([]string{"healthcheck", "extra"})
	if err == nil {
		t.Fatal("parseCommand healthcheck with extra args succeeded")
	}
	if !strings.Contains(err.Error(), "unexpected healthcheck arguments") {
		t.Fatalf("error = %q, want extra args message", err)
	}
}

func TestHealthcheckCommandRunSucceedsAgainstOpenTCPListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			accepted <- err
			return
		}
		accepted <- conn.Close()
	}()

	cmd := healthcheckCommand{
		serverAddr: listener.Addr().String(),
		timeout:    time.Second,
	}
	code, err := cmd.run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if err := <-accepted; err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("accepting healthcheck connection: %v", err)
	}
}

func TestHealthcheckCommandRunFailsAgainstClosedTCPListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := healthcheckCommand{
		serverAddr: addr,
		timeout:    time.Second,
	}
	code, err := cmd.run(t.Context())
	if err == nil {
		t.Fatal("healthcheck against closed listener succeeded")
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
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
