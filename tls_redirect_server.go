package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	tlsHandshakeRecordType = 22
	connClassifyTimeout    = 5 * time.Second
)

// ListenAndServeTLSWithSamePortRedirect serves HTTPS and HTTP->HTTPS redirect on the same TCP port.
func ListenAndServeTLSWithSamePortRedirect(addr, certFile, keyFile string, tlsHandler http.Handler) error {
	baseListener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		_ = baseListener.Close()
		return err
	}

	tlsListener := newConnChanListener(baseListener.Addr())
	httpListener := newConnChanListener(baseListener.Addr())

	httpsServer := &http.Server{
		Handler:           tlsHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpRedirectServer := &http.Server{
		Handler:           newSamePortHTTPSRedirectHandler(addr),
		ReadHeaderTimeout: 5 * time.Second,
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	errCh := make(chan error, 3)

	go func() {
		errCh <- dispatchConnections(baseListener, tlsListener, httpListener)
	}()
	go func() {
		errCh <- httpsServer.Serve(tls.NewListener(tlsListener, tlsConfig))
	}()
	go func() {
		errCh <- httpRedirectServer.Serve(httpListener)
	}()

	err = <-errCh
	_ = baseListener.Close()
	_ = tlsListener.Close()
	_ = httpListener.Close()
	_ = httpsServer.Close()
	_ = httpRedirectServer.Close()

	if err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func dispatchConnections(baseListener net.Listener, tlsListener, httpListener *connChanListener) error {
	for {
		conn, err := baseListener.Accept()
		if err != nil {
			return err
		}

		go classifyAndDispatchConn(conn, tlsListener, httpListener)
	}
}

func classifyAndDispatchConn(conn net.Conn, tlsListener, httpListener *connChanListener) {
	_ = conn.SetReadDeadline(time.Now().Add(connClassifyTimeout))

	first := make([]byte, 1)
	n, readErr := conn.Read(first)
	_ = conn.SetReadDeadline(time.Time{})

	if n == 0 {
		_ = conn.Close()
		return
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		_ = conn.Close()
		return
	}

	wrapped := &prefixedConn{
		Conn:   conn,
		reader: io.MultiReader(bytes.NewReader(first[:n]), conn),
	}

	if first[0] == tlsHandshakeRecordType {
		if !tlsListener.push(wrapped) {
			_ = wrapped.Close()
		}
		return
	}

	if !httpListener.push(wrapped) {
		_ = wrapped.Close()
	}
}

func newSamePortHTTPSRedirectHandler(fallbackAddr string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.TrimSpace(r.Host)
		if host == "" {
			host = fallbackAddr
		}

		targetPath := r.URL.RequestURI()
		if targetPath == "" {
			targetPath = "/"
		}

		http.Redirect(w, r, "https://"+host+targetPath, http.StatusMovedPermanently)
	})
}

type prefixedConn struct {
	net.Conn
	reader io.Reader
}

func (c *prefixedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

type connChanListener struct {
	addr   net.Addr
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newConnChanListener(addr net.Addr) *connChanListener {
	return &connChanListener{
		addr:   addr,
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *connChanListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case conn := <-l.conns:
		return conn, nil
	}
}

func (l *connChanListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *connChanListener) Addr() net.Addr {
	return l.addr
}

func (l *connChanListener) push(conn net.Conn) bool {
	select {
	case <-l.closed:
		return false
	case l.conns <- conn:
		return true
	}
}
