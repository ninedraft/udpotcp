package main

import "testing"

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
