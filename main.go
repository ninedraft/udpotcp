package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ninedraft/udpotcp/internal/tunnel"
)

const defaultPort = 34197

type command interface {
	run(context.Context) (int, error)
}

type clientCommand struct {
	udpListen  string
	serverAddr string
}

type serverCommand struct {
	tcpListen string
	udpRemote string
}

type healthcheckCommand struct {
	serverAddr string
	timeout    time.Duration
}

type clientJSONConfig struct {
	Listen string `json:"listen"`
	Server string `json:"server"`
}

type serverJSONConfig struct {
	Listen string `json:"listen"`
	UDP    string `json:"udp"`
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return parseDiscoveredConfig()
	}

	if isConfigName(args[0]) {
		if len(args) > 1 {
			return nil, fmt.Errorf("unexpected arguments after %s", args[0])
		}
		return parseConfigFile(args[0])
	}

	switch args[0] {
	case "client":
		return parseClientCommand(args[1:])
	case "server":
		return parseServerCommand(args[1:])
	case "healthcheck":
		return parseHealthcheckCommand(args[1:])
	default:
		return nil, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseClientCommand(args []string) (command, error) {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cmd := defaultClientCommand()
	var configPath string
	fs.StringVar(&configPath, "config", "", "JSON config file")
	fs.StringVar(&cmd.udpListen, "listen", cmd.udpListen, "local UDP listen address")
	fs.StringVar(&cmd.serverAddr, "server", cmd.serverAddr, "TCP server address")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected client arguments: %v", fs.Args())
	}

	visited := visitedFlags(fs)
	if configPath != "" {
		loaded, err := loadClientConfig(configPath)
		if err != nil {
			return nil, err
		}
		if visited["listen"] {
			loaded.udpListen = cmd.udpListen
		}
		if visited["server"] {
			loaded.serverAddr = cmd.serverAddr
		}
		cmd = loaded
	}

	if cmd.serverAddr == "" {
		return nil, errors.New("client -server is required")
	}
	return cmd, nil
}

func parseServerCommand(args []string) (command, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cmd := defaultServerCommand()
	var configPath string
	fs.StringVar(&configPath, "config", "", "JSON config file")
	fs.StringVar(&cmd.tcpListen, "listen", cmd.tcpListen, "TCP listen address")
	fs.StringVar(&cmd.udpRemote, "udp", cmd.udpRemote, "UDP endpoint address")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected server arguments: %v", fs.Args())
	}

	visited := visitedFlags(fs)
	if configPath != "" {
		loaded, err := loadServerConfig(configPath)
		if err != nil {
			return nil, err
		}
		if visited["listen"] {
			loaded.tcpListen = cmd.tcpListen
		}
		if visited["udp"] {
			loaded.udpRemote = cmd.udpRemote
		}
		cmd = loaded
	}

	if cmd.udpRemote == "" {
		return nil, errors.New("server -udp is required")
	}
	return cmd, nil
}

func parseHealthcheckCommand(args []string) (command, error) {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cmd := defaultHealthcheckCommand()
	fs.StringVar(&cmd.serverAddr, "server", cmd.serverAddr, "TCP server address")
	fs.DurationVar(&cmd.timeout, "timeout", cmd.timeout, "TCP dial timeout")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected healthcheck arguments: %v", fs.Args())
	}
	return cmd, nil
}

func parseDiscoveredConfig() (command, error) {
	serverExists, err := fileExists("server.json")
	if err != nil {
		return nil, err
	}
	clientExists, err := fileExists("client.json")
	if err != nil {
		return nil, err
	}

	switch {
	case serverExists && clientExists:
		return nil, errors.New("both server.json and client.json exist; use an explicit subcommand or config path")
	case serverExists:
		return loadServerConfig("server.json")
	case clientExists:
		return loadClientConfig("client.json")
	default:
		return nil, errors.New("usage: udpotcp client -server host:34197 | udpotcp server -udp host:34197 | udpotcp server.json | udpotcp client.json")
	}
}

func parseConfigFile(path string) (command, error) {
	switch filepath.Base(path) {
	case "server.json":
		return loadServerConfig(path)
	case "client.json":
		return loadClientConfig(path)
	default:
		return nil, fmt.Errorf("unknown config file %q", path)
	}
}

func loadClientConfig(path string) (clientCommand, error) {
	cfg := clientJSONConfig{}
	if err := decodeJSONFile(path, &cfg); err != nil {
		return clientCommand{}, err
	}
	cmd := defaultClientCommand()
	if cfg.Listen != "" {
		cmd.udpListen = cfg.Listen
	}
	cmd.serverAddr = cfg.Server
	if cmd.serverAddr == "" {
		return clientCommand{}, errors.New("client -server is required")
	}
	return cmd, nil
}

func loadServerConfig(path string) (serverCommand, error) {
	cfg := serverJSONConfig{}
	if err := decodeJSONFile(path, &cfg); err != nil {
		return serverCommand{}, err
	}
	cmd := defaultServerCommand()
	if cfg.Listen != "" {
		cmd.tcpListen = cfg.Listen
	}
	cmd.udpRemote = cfg.UDP
	if cmd.udpRemote == "" {
		return serverCommand{}, errors.New("server -udp is required")
	}
	return cmd, nil
}

func decodeJSONFile(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("reading %s: unexpected data after JSON object", path)
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("checking %s: %w", path, err)
}

func isConfigName(path string) bool {
	switch filepath.Base(path) {
	case "server.json", "client.json":
		return true
	default:
		return false
	}
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})
	return visited
}

func defaultClientCommand() clientCommand {
	return clientCommand{
		udpListen: net.JoinHostPort("127.0.0.1", fmt.Sprint(defaultPort)),
	}
}

func defaultServerCommand() serverCommand {
	return serverCommand{
		tcpListen: net.JoinHostPort("", fmt.Sprint(defaultPort)),
	}
}

func defaultHealthcheckCommand() healthcheckCommand {
	return healthcheckCommand{
		serverAddr: net.JoinHostPort("127.0.0.1", fmt.Sprint(defaultPort)),
		timeout:    5 * time.Second,
	}
}

func (cmd clientCommand) config(udpConn net.PacketConn) tunnel.ClientConfig {
	return tunnel.ClientConfig{
		UDPListen:  cmd.udpListen,
		UDPConn:    udpConn,
		ServerAddr: cmd.serverAddr,
		Log:        newHumanLogger(os.Stderr),
	}
}

func (cmd clientCommand) run(ctx context.Context) (int, error) {
	return 0, tunnel.RunClient(ctx, cmd.config(nil))
}

func (cmd serverCommand) config(listener net.Listener) tunnel.ServerConfig {
	return tunnel.ServerConfig{
		TCPListen: cmd.tcpListen,
		Listener:  listener,
		UDPRemote: cmd.udpRemote,
		Log:       newHumanLogger(os.Stderr),
	}
}

func (cmd serverCommand) run(ctx context.Context) (int, error) {
	return 0, tunnel.RunServer(ctx, cmd.config(nil))
}

func (cmd healthcheckCommand) run(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, cmd.timeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", cmd.serverAddr)
	if err != nil {
		return 1, fmt.Errorf("healthcheck tcp %s: %w", cmd.serverAddr, err)
	}
	return 0, conn.Close()
}

func newHumanLogger(w io.Writer) *log.Logger {
	return log.New(w, "", log.LstdFlags)
}

func contextWithSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func run(args []string) (int, error) {
	cmd, err := parseCommand(args)
	if err != nil {
		return 2, err
	}
	ctx, stop := contextWithSignal()
	defer stop()
	return cmd.run(ctx)
}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		code = cmp.Or(code, 1)
	}

	if code != 0 {
		os.Exit(code)
	}
}
