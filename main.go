package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

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

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: udpotcp client -server host:34197 | udpotcp server -udp host:34197")
	}

	switch args[0] {
	case "client":
		fs := flag.NewFlagSet("client", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cmd := clientCommand{}
		fs.StringVar(&cmd.udpListen, "listen", net.JoinHostPort("127.0.0.1", fmt.Sprint(defaultPort)), "local UDP listen address")
		fs.StringVar(&cmd.serverAddr, "server", "", "TCP server address")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, err
		}
		if cmd.serverAddr == "" {
			return nil, errors.New("client -server is required")
		}
		return cmd, nil
	case "server":
		fs := flag.NewFlagSet("server", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cmd := serverCommand{}
		fs.StringVar(&cmd.tcpListen, "listen", net.JoinHostPort("", fmt.Sprint(defaultPort)), "TCP listen address")
		fs.StringVar(&cmd.udpRemote, "udp", "", "UDP endpoint address")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, err
		}
		if cmd.udpRemote == "" {
			return nil, errors.New("server -udp is required")
		}
		return cmd, nil
	default:
		return nil, fmt.Errorf("unknown command %q", args[0])
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
