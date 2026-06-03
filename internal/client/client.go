//go:build linux

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/nobu/com2wsl/internal/debug"
	"github.com/nobu/com2wsl/internal/protocol"
	"github.com/nobu/com2wsl/internal/winhost"
)

const (
	pollInterval   = 200 * time.Millisecond
	openConfirm    = 2  // consecutive polls
	closeConfirm   = 3  // consecutive polls
	releaseCooldown = 500 * time.Millisecond
)

var errDeviceClosed = errors.New("device closed")

// Config holds client runtime options.
type Config struct {
	ServerAddr   string
	ControlPort  int
	BasePort     int
	LinkDir      string
	SyncInterval time.Duration
}

// Run syncs ports from the server and maintains local pseudo-serial links.
func Run(ctx context.Context, cfg Config) error {
	controlPort := cfg.ControlPort
	if controlPort == 0 {
		controlPort = protocol.DefaultControlPort
	}
	serverAddr := cfg.ServerAddr
	if serverAddr == "" {
		var derr error
		serverAddr, derr = winhost.DiscoverServer(controlPort)
		if derr != nil {
			return derr
		}
	}
	if cfg.LinkDir == "" {
		cfg.LinkDir = filepath.Join(mustHome(), ".com2wsl")
	}
	if err := os.MkdirAll(cfg.LinkDir, 0o755); err != nil {
		return err
	}

	log.Printf("com2wsl client: server=http://%s link-dir=%s", serverAddr, cfg.LinkDir)

	mgr := &sessionManager{
		serverAddr: serverAddr,
		basePort:   cfg.BasePort,
		linkDir:    cfg.LinkDir,
		sessions:   make(map[int]*portSession),
	}

	t := time.NewTicker(cfg.SyncInterval)
	defer t.Stop()

	mgr.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			mgr.shutdown()
			return nil
		case <-t.C:
			mgr.sync(ctx)
		}
	}
}

func mustHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

type sessionManager struct {
	serverAddr string
	basePort   int
	linkDir    string
	mu         sync.Mutex
	sessions   map[int]*portSession
}

func (m *sessionManager) sync(ctx context.Context) {
	ports, err := fetchPorts(ctx, m.serverAddr)
	if err != nil {
		log.Printf("com2wsl client: sync: %v", err)
		return
	}
	seen := make(map[int]struct{})
	for _, p := range ports {
		seen[p.Number] = struct{}{}
		m.mu.Lock()
		if _, ok := m.sessions[p.Number]; !ok {
			s := &portSession{
				info:     p,
				linkPath: filepath.Join(m.linkDir, p.Name),
				host:     hostOnly(m.serverAddr),
				pid:      os.Getpid(),
			}
			m.sessions[p.Number] = s
			go s.run(ctx, m.basePort)
			log.Printf("com2wsl client: %s -> %s (tcp :%d)", p.Name, s.linkPath, p.DataPort)
		} else {
			m.sessions[p.Number].info = p
		}
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for n, s := range m.sessions {
		if _, ok := seen[n]; !ok {
			s.stop()
			delete(m.sessions, n)
			log.Printf("com2wsl client: removed %s", s.info.Name)
		}
	}
}

func hostOnly(serverAddr string) string {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return serverAddr
	}
	return host
}

func fetchPorts(ctx context.Context, serverAddr string) ([]protocol.PortInfo, error) {
	url := fmt.Sprintf("http://%s/api/ports", serverAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/ports: %s: %s", resp.Status, string(body))
	}
	var pr protocol.PortsResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return pr.Ports, nil
}

func (m *sessionManager) shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.stop()
	}
	m.sessions = make(map[int]*portSession)
}

type portSession struct {
	info     protocol.PortInfo
	linkPath string
	host     string
	pid      int

	cancel context.CancelFunc
	mu     sync.Mutex

	master *os.File
}

func (s *portSession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.master != nil {
		_ = s.master.Close()
		s.master = nil
	}
	_ = os.Remove(s.linkPath)
}

func (s *portSession) run(parent context.Context, basePort int) {
	if err := s.setupPTY(); err != nil {
		log.Printf("com2wsl client: %s setup: %v", s.info.Name, err)
		return
	}
	defer s.teardownPTY()

	for {
		if parent.Err() != nil {
			return
		}

		ctx, cancel := context.WithCancel(parent)
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()

		if !fdHeldByApp(s.linkPath, s.pid) {
			if err := waitForAppOpen(ctx, s.linkPath, s.pid); err != nil {
				cancel()
				if parent.Err() != nil {
					return
				}
				continue
			}
		}
		debug.Logf("com2wsl client: %s serial opened %s", s.info.Name, s.linkPath)

		err := s.relay(ctx, basePort)
		cancel()

		if parent.Err() != nil {
			return
		}
		if err != nil {
			debug.Logf("com2wsl client: %s tcp disconnected: %v", s.info.Name, err)
		}

		// relay 结束即表示本轮会话结束（device closed / ptmx EIO / tcp reset）。
		// 不再 waitForAppClosed：apply 配置时会极快关串口再开，若此时 fd 已再次被占用，
		// waitForAppClosed 会永远等「第二次 close」而卡死。
		select {
		case <-parent.Done():
			return
		case <-time.After(releaseCooldown):
		}
	}
}

func (s *portSession) setupPTY() error {
	master, slave, err := pty.Open()
	if err != nil {
		return err
	}
	pts, err := ptsName(slave)
	if err != nil {
		_ = master.Close()
		_ = slave.Close()
		return err
	}
	_ = slave.Close()
	_ = os.Remove(s.linkPath)
	if err := os.Symlink(pts, s.linkPath); err != nil {
		_ = master.Close()
		return fmt.Errorf("symlink %s -> %s: %w", s.linkPath, pts, err)
	}
	s.mu.Lock()
	s.master = master
	s.mu.Unlock()
	debug.Logf("com2wsl client: %s pty ready %s -> %s", s.info.Name, s.linkPath, pts)
	return nil
}

func (s *portSession) teardownPTY() {
	s.mu.Lock()
	if s.master != nil {
		_ = s.master.Close()
		s.master = nil
	}
	s.mu.Unlock()
	_ = os.Remove(s.linkPath)
}

func (s *portSession) relay(ctx context.Context, basePort int) error {
	s.mu.Lock()
	master := s.master
	s.mu.Unlock()
	if master == nil {
		return fmt.Errorf("pty not ready")
	}

	dataPort := s.info.DataPort
	if dataPort == 0 {
		dataPort = protocol.DataPort(basePort, s.info.Number)
	}
	target := net.JoinHostPort(s.host, fmt.Sprintf("%d", dataPort))

	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return fmt.Errorf("tcp %s: %w", target, err)
	}

	debug.Logf("com2wsl client: %s tcp connected %s", s.info.Name, target)

	relayCtx, relayCancel := context.WithCancel(ctx)
	defer relayCancel()

	// 任一方向结束立刻关闭 TCP 并打断另一侧 Copy（与 server 端对称）。
	var closeOnce sync.Once
	closeRelay := func() {
		closeOnce.Do(func() {
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
			_ = conn.Close()
			debug.Logf("com2wsl client: %s tcp closed %s", s.info.Name, target)
		})
	}
	defer closeRelay()

	errCh := make(chan error, 3)
	var copyWg sync.WaitGroup
	copyWg.Add(2)
	go func() {
		defer copyWg.Done()
		defer closeRelay()
		_, err := io.Copy(conn, master)
		errCh <- err
	}()
	go func() {
		defer copyWg.Done()
		defer closeRelay()
		_, err := io.Copy(master, conn)
		errCh <- err
	}()
	go func() {
		if err := watchAppClose(relayCtx, s.linkPath, s.pid); err != nil {
			errCh <- err
			return
		}
		errCh <- errDeviceClosed
	}()

	var relayErr error
	select {
	case <-ctx.Done():
		relayErr = ctx.Err()
	case relayErr = <-errCh:
	}

	relayCancel()
	closeRelay()
	copyWg.Wait()

	if relayErr != nil && ctx.Err() == nil && !errors.Is(relayErr, errDeviceClosed) {
		return relayErr
	}
	return ctx.Err()
}

func waitForAppOpen(ctx context.Context, linkPath string, pid int) error {
	confirmed := 0
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if fdHeldByApp(linkPath, pid) {
				confirmed++
				if confirmed >= openConfirm {
					return nil
				}
			} else {
				confirmed = 0
			}
		}
	}
}

func watchAppClose(ctx context.Context, linkPath string, pid int) error {
	confirmed := 0
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if fdHeldByApp(linkPath, pid) {
				confirmed = 0
			} else {
				confirmed++
				if confirmed >= closeConfirm {
					return nil
				}
			}
		}
	}
}

func ptsName(f *os.File) (string, error) {
	return os.Readlink("/proc/self/fd/" + fmt.Sprint(f.Fd()))
}
