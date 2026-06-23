package tunnel

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestTunnelForwardsUDPDatagramAndReply(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	udpEndpoint, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udpEndpoint.Close()

	serverAddr, serverDone := startServerForTest(t, ctx, udpEndpoint.LocalAddr().String(), io.Discard)

	clientUDPAddr, clientDone := startClientForTest(t, ctx, serverAddr, t.Output())

	localApp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localApp.Close()

	if _, err := localApp.WriteTo([]byte("ping"), clientUDPAddr); err != nil {
		t.Fatal(err)
	}

	udpEndpointBuf := make([]byte, 1024)
	if err := udpEndpoint.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, serverSideUDPAddr, err := udpEndpoint.ReadFrom(udpEndpointBuf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(udpEndpointBuf[:n]); got != "ping" {
		t.Fatalf("UDP endpoint got %q, want %q", got, "ping")
	}

	if _, err := udpEndpoint.WriteTo([]byte("pong"), serverSideUDPAddr); err != nil {
		t.Fatal(err)
	}

	appBuf := make([]byte, 1024)
	if err := localApp.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err = localApp.ReadFrom(appBuf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(appBuf[:n]); got != "pong" {
		t.Fatalf("local app got %q, want %q", got, "pong")
	}

	cancel()
	waitNoError(t, serverDone)
	waitNoError(t, clientDone)
}

func TestServerUsesIndependentUDPSocketPerTCPClient(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	udpEndpoint, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udpEndpoint.Close()

	serverAddr, serverDone := startServerForTest(t, ctx, udpEndpoint.LocalAddr().String(), io.Discard)

	clientA, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Close()
	clientB, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Close()

	if err := WriteFrame(clientA, []byte("from-a")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(clientB, []byte("from-b")); err != nil {
		t.Fatal(err)
	}

	seen := map[string]net.Addr{}
	for range 2 {
		buf := make([]byte, 1024)
		if err := udpEndpoint.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		n, addr, err := udpEndpoint.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		seen[string(buf[:n])] = addr
	}

	if seen["from-a"] == nil || seen["from-b"] == nil {
		t.Fatalf("UDP endpoint saw packets %v, want from-a and from-b", seen)
	}
	if seen["from-a"].String() == seen["from-b"].String() {
		t.Fatalf("server reused UDP source %s for both TCP clients", seen["from-a"])
	}

	if _, err := udpEndpoint.WriteTo([]byte("reply-a"), seen["from-a"]); err != nil {
		t.Fatal(err)
	}

	replyBuf := make([]byte, MaxDatagramSize)
	if err := clientA.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	reply, err := ReadFrame(clientA, replyBuf)
	if err != nil {
		t.Fatal(err)
	}
	if string(reply) != "reply-a" {
		t.Fatalf("client A got %q, want reply-a", reply)
	}

	if err := clientB.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFrame(clientB, replyBuf)
	if err == nil {
		t.Fatal("client B received client A reply")
	}

	cancel()
	waitNoError(t, serverDone)
}

func TestSleepContextUsesFakeTimeUnderSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		done := make(chan error)

		go func() {
			done <- sleepContext(t.Context(), 5*time.Second)
		}()

		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("sleepContext returned early: %v", err)
		default:
		}

		time.Sleep(5 * time.Second)
		synctest.Wait()

		if got := time.Since(start); got != 5*time.Second {
			t.Fatalf("elapsed fake time = %s, want 5s", got)
		}
		if err := <-done; err != nil {
			t.Fatalf("sleepContext() error = %v", err)
		}
	})
}

func TestClientDialRetriesUseSynctestFakeTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()

		attempts := 0
		done := make(chan error)
		go func() {
			done <- RunClient(ctx, ClientConfig{
				UDPConn:      fakePacketConn{addr: fakeAddr("127.0.0.1:34197")},
				ServerAddr:   "server.example:34197",
				RetryBackoff: 5 * time.Second,
				Log:          log.New(io.Discard, "", 0),
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					attempts++
					return nil, errors.New("dial failed")
				},
			})
		}()

		synctest.Wait()
		if attempts != 1 {
			t.Fatalf("attempts after initial dial = %d, want 1", attempts)
		}

		time.Sleep(5 * time.Second)
		synctest.Wait()
		if attempts != 2 {
			t.Fatalf("attempts after first backoff = %d, want 2", attempts)
		}

		time.Sleep(5 * time.Second)
		synctest.Wait()
		if attempts != 3 {
			t.Fatalf("attempts after second backoff = %d, want 3", attempts)
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("RunClient() error = %v, want context canceled", err)
		}
	})
}

func startServerForTest(t *testing.T, ctx context.Context, udpRemote string, logOutput io.Writer) (string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- RunServer(ctx, ServerConfig{
			Listener:  listener,
			UDPRemote: udpRemote,
			Log:       log.New(logOutput, "", 0),
		})
	}()

	return listener.Addr().String(), done
}

func startClientForTest(t *testing.T, ctx context.Context, serverAddr string, logOutput io.Writer) (net.Addr, <-chan error) {
	t.Helper()

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udpConn.Close() })

	done := make(chan error, 1)
	go func() {
		done <- RunClient(ctx, ClientConfig{
			UDPConn:      udpConn,
			ServerAddr:   serverAddr,
			RetryBackoff: 10 * time.Millisecond,
			Log:          log.New(logOutput, "", 0),
		})
	}()

	return udpConn.LocalAddr(), done
}

func waitNoError(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner to stop")
	}
}

type fakePacketConn struct {
	addr net.Addr
}

func (c fakePacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	select {}
}

func (c fakePacketConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, errors.New("unexpected WriteTo")
}

func (c fakePacketConn) Close() error {
	return nil
}

func (c fakePacketConn) LocalAddr() net.Addr {
	return c.addr
}

func (c fakePacketConn) SetDeadline(time.Time) error {
	return nil
}

func (c fakePacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c fakePacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

type fakeAddr string

func (a fakeAddr) Network() string { return "udp" }
func (a fakeAddr) String() string  { return string(a) }
