package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/netutil"
)

type ClientConfig struct {
	UDPListen    string
	UDPConn      net.PacketConn
	ServerAddr   string
	RetryBackoff time.Duration
	Log          *log.Logger
	DialContext  func(context.Context, string, string) (net.Conn, error)
}

type ServerConfig struct {
	TCPListen string
	Listener  net.Listener
	UDPRemote string
	Log       *log.Logger
}

func RunClient(ctx context.Context, cfg ClientConfig) error {
	if cfg.ServerAddr == "" {
		return errors.New("server address is required")
	}
	if cfg.UDPListen == "" {
		cfg.UDPListen = "127.0.0.1:34197"
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Second
	}
	logf := logger(cfg.Log)

	udpConn := cfg.UDPConn
	ownsUDPConn := udpConn == nil
	if udpConn == nil {
		conn, err := net.ListenPacket("udp", cfg.UDPListen)
		if err != nil {
			return fmt.Errorf("listening udp %s: %w", cfg.UDPListen, err)
		}
		udpConn = conn
	}
	if ownsUDPConn {
		defer udpConn.Close()
	}

	logf("udp listen %s", udpConn.LocalAddr())

	dialContext := cfg.DialContext
	if dialContext == nil {
		var dialer net.Dialer
		dialContext = dialer.DialContext
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		tcpConn, err := dialContext(ctx, "tcp", cfg.ServerAddr)
		if err != nil {
			logf("tcp retry %s after %s: %v", cfg.ServerAddr, cfg.RetryBackoff, err)
			if err := sleepContext(ctx, cfg.RetryBackoff); err != nil {
				return err
			}
			continue
		}

		logf("tcp connected %s -> %s", tcpConn.LocalAddr(), tcpConn.RemoteAddr())
		err = runClientSession(ctx, udpConn, tcpConn, logf)
		_ = tcpConn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !isShutdownError(err) {
			logf("tcp disconnected %s: %v", cfg.ServerAddr, err)
		}
		logf("tcp retry %s after %s", cfg.ServerAddr, cfg.RetryBackoff)
		if err := sleepContext(ctx, cfg.RetryBackoff); err != nil {
			return err
		}
	}
}

const serverDefaultMaxConnections = 16

func RunServer(ctx context.Context, cfg ServerConfig) error {
	if cfg.UDPRemote == "" {
		return errors.New("udp remote address is required")
	}
	if cfg.TCPListen == "" {
		cfg.TCPListen = ":34197"
	}
	logf := logger(cfg.Log)

	listener := cfg.Listener
	ownsListener := listener == nil
	if listener == nil {
		var err error
		listener, err = net.Listen("tcp", cfg.TCPListen)
		if err != nil {
			return fmt.Errorf("listening tcp %s: %w", cfg.TCPListen, err)
		}

		listener = netutil.LimitListener(listener, serverDefaultMaxConnections)
	}
	if ownsListener {
		defer listener.Close()
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	logf("tcp listen %s", listener.Addr())
	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return ctx.Err()
			}
			return fmt.Errorf("accepting tcp: %w", err)
		}

		logf("tcp client %s connected", tcpConn.RemoteAddr())
		go func() {
			if err := runServerClient(ctx, tcpConn, cfg.UDPRemote, logf); err != nil && !isShutdownError(err) {
				logf("tcp client %s closed: %v", tcpConn.RemoteAddr(), err)
			}
		}()
	}
}

func runClientSession(ctx context.Context, udpConn net.PacketConn, tcpConn net.Conn, logf func(string, ...any)) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var peerMu sync.RWMutex
	var peer net.Addr
	errc := make(chan error, 2)

	logf("tcp -> %s", tcpConn.RemoteAddr())
	defer logf("tcp -> %s closed", tcpConn.RemoteAddr())

	go func() {
		buf := make([]byte, MaxDatagramSize)
		for {
			if err := sessionCtx.Err(); err != nil {
				errc <- err
				return
			}

			_ = udpConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, addr, err := udpConn.ReadFrom(buf)
			if err != nil {
				if timeoutError(err) {
					continue
				}
				errc <- err
				return
			}

			peerMu.Lock()
			peer = addr
			peerMu.Unlock()

			if err := WriteFrame(tcpConn, buf[:n]); err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, MaxDatagramSize)
		for {
			payload, err := ReadFrame(tcpConn, buf)
			if err != nil {
				errc <- err
				return
			}

			peerMu.RLock()
			addr := peer
			peerMu.RUnlock()
			if addr == nil {
				logf("udp drop %dB: no local peer yet", len(payload))
				continue
			}

			if _, err := udpConn.WriteTo(payload, addr); err != nil {
				errc <- err
				return
			}
		}
	}()

	err := <-errc
	cancel()
	_ = tcpConn.Close()
	return err
}

func runServerClient(ctx context.Context, tcpConn net.Conn, udpRemote string, logf func(string, ...any)) error {
	defer tcpConn.Close()

	udpConn, err := net.Dial("udp", udpRemote)
	if err != nil {
		return fmt.Errorf("dialing udp %s: %w", udpRemote, err)
	}
	defer udpConn.Close()

	logf("udp connected %s -> %s", udpConn.LocalAddr(), udpConn.RemoteAddr())
	defer logf("udp disconnected %s -> %s", udpConn.LocalAddr(), udpConn.RemoteAddr())

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)

	go func() {
		buf := make([]byte, MaxDatagramSize)
		for {
			payload, err := ReadFrame(tcpConn, buf)
			if err != nil {
				errc <- err
				return
			}
			if _, err := udpConn.Write(payload); err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, MaxDatagramSize)
		for {
			if err := sessionCtx.Err(); err != nil {
				errc <- err
				return
			}

			_ = udpConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, err := udpConn.Read(buf)
			if err != nil {
				if timeoutError(err) {
					continue
				}
				errc <- err
				return
			}

			if err := WriteFrame(tcpConn, buf[:n]); err != nil {
				errc <- err
				return
			}
		}
	}()

	err = <-errc
	cancel()
	_ = tcpConn.Close()
	return err
}

func logger(l *log.Logger) func(string, ...any) {
	if l == nil {
		l = log.New(os.Stderr, "", log.LstdFlags)
	}
	return l.Printf
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func timeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isShutdownError(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF)
}
