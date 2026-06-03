//go:build windows

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"github.com/nobu/com2wsl/internal/debug"
	"github.com/nobu/com2wsl/internal/protocol"
	"github.com/nobu/com2wsl/internal/serialcfg"
)

// Config holds server runtime options.
type Config struct {
	BindAddr      string
	ControlPort   int
	BasePort      int
	ScanInterval  time.Duration
	Serial        serialcfg.Settings
}

// Run starts the control API and port relays until ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	reg := newRegistry(cfg.BasePort, cfg.Serial)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/ports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.PortsResponse{Ports: reg.List()})
	})

	controlAddr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.ControlPort)
	httpServer := &http.Server{Addr: controlAddr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("com2wsl server: control http://%s/api/ports", controlAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	go reg.scanLoop(scanCtx, cfg.ScanInterval)

	<-ctx.Done()
	cancelScan()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	reg.shutdown()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

type registry struct {
	basePort int
	serial   serialcfg.Settings
	mu       sync.RWMutex
	ports    map[int]*portState
}

type portState struct {
	name        string
	number      int
	description string
	listener    net.Listener
	relayBusy   bool
	lastError   string
	mu          sync.Mutex
}

func newRegistry(basePort int, s serialcfg.Settings) *registry {
	return &registry{
		basePort: basePort,
		serial:   s,
		ports:    make(map[int]*portState),
	}
}

func (r *registry) List() []protocol.PortInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]protocol.PortInfo, 0, len(r.ports))
	for _, p := range r.ports {
		out = append(out, p.info(r.basePort))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

func (ps *portState) info(base int) protocol.PortInfo {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	status := "idle"
	if ps.relayBusy {
		status = "active"
	} else if ps.lastError != "" {
		status = "busy"
	}
	return protocol.PortInfo{
		Name:        ps.name,
		Number:      ps.number,
		DataPort:    protocol.DataPort(base, ps.number),
		Status:      status,
		Description: ps.description,
		Error:       ps.lastError,
	}
}

func (r *registry) scanLoop(ctx context.Context, interval time.Duration) {
	r.rescan()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.rescan()
		}
	}
}

func (r *registry) rescan() {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Printf("com2wsl server: enumerate ports: %v", err)
		ports = nil
	}
	seen := make(map[int]struct{})
	for _, p := range ports {
		if p == nil || p.Name == "" {
			continue
		}
		n, err := protocol.ParseCOMName(p.Name)
		if err != nil {
			continue
		}
		seen[n] = struct{}{}
		r.ensurePort(p.Name, n, p.Product)
	}
	r.removeExcept(seen)
}

func (r *registry) ensurePort(name string, number int, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.ports[number]; ok {
		existing.name = name
		if description != "" {
			existing.description = description
		}
		return
	}
	ps := &portState{name: name, number: number, description: description}
	r.ports[number] = ps
	go r.listenPort(ps)
}

func (r *registry) listenPort(ps *portState) {
	addr := fmt.Sprintf("0.0.0.0:%d", protocol.DataPort(r.basePort, ps.number))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ps.mu.Lock()
		ps.lastError = err.Error()
		ps.mu.Unlock()
		log.Printf("com2wsl server: listen %s: %v", addr, err)
		return
	}
	ps.mu.Lock()
	ps.listener = ln
	ps.lastError = ""
	ps.mu.Unlock()
	log.Printf("com2wsl server: %s -> tcp %s", ps.name, addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go r.handleConn(ps, conn)
	}
}

func (r *registry) handleConn(ps *portState, conn net.Conn) {
	ps.mu.Lock()
	if ps.relayBusy {
		ps.mu.Unlock()
		debug.Logf("com2wsl server: %s tcp rejected (busy) from %s", ps.name, conn.RemoteAddr())
		_ = conn.Close()
		return
	}
	ps.relayBusy = true
	ps.lastError = ""
	ps.mu.Unlock()
	remote := conn.RemoteAddr().String()
	debug.Logf("com2wsl server: %s tcp connected from %s", ps.name, remote)
	defer func() {
		ps.mu.Lock()
		ps.relayBusy = false
		ps.mu.Unlock()
		_ = conn.Close()
		debug.Logf("com2wsl server: %s tcp disconnected from %s", ps.name, remote)
	}()

	port, err := serial.Open(ps.name, r.serial.Mode())
	if err != nil {
		ps.mu.Lock()
		ps.lastError = err.Error()
		ps.mu.Unlock()
		debug.Logf("com2wsl server: %s serial open failed: %v", ps.name, err)
		log.Printf("com2wsl server: open %s: %v", ps.name, err)
		return
	}
	defer func() {
		_ = port.Close()
		debug.Logf("com2wsl server: %s serial closed", ps.name)
	}()

	debug.Logf("com2wsl server: %s serial opened", ps.name)

	// 任一方向结束立刻关闭串口和 TCP，避免另一侧 io.Copy 永久阻塞在串口 Read 上，
	// 导致 relayBusy 无法清零、Windows COM 不释放（第二次连接 tcp rejected busy）。
	var closeOnce sync.Once
	closeRelay := func() {
		closeOnce.Do(func() {
			_ = port.Close()
			_ = conn.Close()
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer closeRelay()
		_, _ = io.Copy(conn, port)
	}()
	go func() {
		defer wg.Done()
		defer closeRelay()
		_, _ = io.Copy(port, conn)
	}()
	wg.Wait()
}

func (r *registry) removeExcept(seen map[int]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for n, ps := range r.ports {
		if _, ok := seen[n]; ok {
			continue
		}
		if ps.listener != nil {
			_ = ps.listener.Close()
		}
		delete(r.ports, n)
		log.Printf("com2wsl server: removed %s", ps.name)
	}
}

func (r *registry) shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ps := range r.ports {
		if ps.listener != nil {
			_ = ps.listener.Close()
		}
	}
}
