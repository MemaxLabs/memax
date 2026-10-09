// Package netsim puts a slow network between integration tests and their
// Postgres, and counts the round trips a pool makes over it.
//
// Production runs the API on Fly in sjc and Postgres on Neon in us-west-2:
// a `SELECT 1` takes about 24 ms there (measured Oct 7, 2026), against well
// under a millisecond on a local database. Every V2 latency number taken on
// a local database hides that cost, so:
//
//   - Proxy is a TCP proxy that holds every byte for a fixed one-way delay
//     in each direction (12 ms each way is production's 24 ms round trip).
//     It also counts round trips on the wire, and can hand each statement
//     it forwards to a watcher (the RLS-ordering audit, audit.go).
//   - Tracer is a pgx tracer that counts the round trips each operation
//     makes: queries, batches (one pipeline flush each), prepares, COPYs
//     and the pool's liveness pings. Wall clock is noisy on shared CI
//     machines; round-trip counts are not, so the CI guards assert those.
//
// testdb.Open wires both onto a per-test database.
package netsim

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Proxy forwards TCP connections to a target, delaying each direction.
type Proxy struct {
	ln     net.Listener
	target string
	oneWay atomic.Int64 // nanoseconds

	// trips counts client bursts that follow a server response, over every
	// connection: what a round trip is on the wire.
	trips atomic.Int64
	ids   atomic.Int64

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	watch  func(Statement)
	closed bool
	wg     sync.WaitGroup
}

// Statement is one statement a client sent: a simple query's text, or an
// extended-protocol Bind of a parsed statement with its parameters (nil
// for NULL). Conn numbers the proxied connection.
type Statement struct {
	Conn   int64
	SQL    string
	Params [][]byte
}

// Listen starts a proxy on a free localhost port in front of target
// (host:port), delaying each direction by oneWay.
func Listen(target string, oneWay time.Duration) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{ln: ln, target: target, conns: map[net.Conn]struct{}{}}
	p.oneWay.Store(int64(oneWay))
	p.wg.Add(1)
	go p.accept()
	return p, nil
}

// Addr is the proxy's host:port.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// OneWay is the delay each direction adds.
func (p *Proxy) OneWay() time.Duration { return time.Duration(p.oneWay.Load()) }

// SetOneWay changes the delay; bytes already in flight keep theirs.
func (p *Proxy) SetOneWay(d time.Duration) { p.oneWay.Store(int64(d)) }

// RoundTrips counts, over every connection so far, the times a client
// started sending after the server had answered: the round trips on the
// wire, connection setup included.
func (p *Proxy) RoundTrips() int64 { return p.trips.Load() }

// Watch hands every statement clients send to fn, in each connection's
// order (one goroutine per connection calls it). Set it before the pool
// connects.
func (p *Proxy) Watch(fn func(Statement)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watch = fn
}

// Close stops accepting, cuts every connection and waits for the pipes.
func (p *Proxy) Close() error {
	p.mu.Lock()
	p.closed = true
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	err := p.ln.Close()
	p.wg.Wait()
	return err
}

func (p *Proxy) accept() {
	defer p.wg.Done()
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.serve(client)
		}()
	}
}

func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.conns, c)
}

func (p *Proxy) serve(client net.Conn) {
	server, err := net.DialTimeout("tcp", p.target, 5*time.Second)
	if err != nil {
		_ = client.Close()
		return
	}
	if !p.track(client) || !p.track(server) {
		_ = client.Close()
		_ = server.Close()
		return
	}
	defer p.untrack(client)
	defer p.untrack(server)

	p.mu.Lock()
	watch := p.watch
	p.mu.Unlock()
	id := p.ids.Add(1)
	var fe *frontend
	if watch != nil {
		fe = &frontend{conn: id, emit: watch, stmts: map[string]string{}}
	}

	// The wire round-trip count: a client burst that follows server data.
	var mu sync.Mutex
	serverSpoke, sent := false, false
	fromClient := func(b []byte) {
		mu.Lock()
		if !sent || serverSpoke {
			p.trips.Add(1)
		}
		sent, serverSpoke = true, false
		mu.Unlock()
		if fe != nil {
			fe.feed(b)
		}
	}
	fromServer := func([]byte) {
		mu.Lock()
		serverSpoke = true
		mu.Unlock()
	}

	done := make(chan struct{}, 2)
	go func() { p.pipe(server, client, fromClient); done <- struct{}{} }()
	go func() { p.pipe(client, server, fromServer); done <- struct{}{} }()
	<-done
	// Either side hanging up ends the connection.
	_ = client.Close()
	_ = server.Close()
	<-done
}

type chunk struct {
	b  []byte
	at time.Time
}

// pipe copies src to dst, each chunk leaving oneWay after it arrived, so
// the delay is pure latency: pipelined bytes keep flowing.
func (p *Proxy) pipe(dst, src net.Conn, seen func([]byte)) {
	q := make(chan chunk, 4096)
	go func() {
		defer close(q)
		for {
			buf := make([]byte, 32<<10)
			n, err := src.Read(buf)
			if n > 0 {
				seen(buf[:n])
				q <- chunk{b: buf[:n], at: time.Now()}
			}
			if err != nil {
				return
			}
		}
	}()
	for c := range q {
		if d := time.Until(c.at.Add(p.OneWay())); d > 0 {
			time.Sleep(d)
		}
		if _, err := dst.Write(c.b); err != nil {
			_ = src.Close()
			// Drain, so the reader can exit.
			for range q {
			}
			return
		}
	}
}

// Protocol codes of the untyped messages a connection starts with.
const (
	codeSSLRequest    = 80877103
	codeGSSENCRequest = 80877104
	codeCancelRequest = 80877102
)

// frontend decodes the client's side of the protocol enough to name each
// statement: simple queries, and Parse + Bind of extended ones.
type frontend struct {
	conn    int64
	emit    func(Statement)
	buf     []byte
	started bool
	opaque  bool
	stmts   map[string]string
}

func (f *frontend) feed(data []byte) {
	if f.opaque {
		return
	}
	f.buf = append(f.buf, data...)
	for {
		if !f.started {
			if len(f.buf) < 8 {
				return
			}
			n := int(binary.BigEndian.Uint32(f.buf[:4]))
			if n < 8 || len(f.buf) < n {
				return
			}
			code := binary.BigEndian.Uint32(f.buf[4:8])
			f.buf = f.buf[n:]
			switch code {
			case codeSSLRequest:
				// TLS may follow; the audit can't read it.
				f.opaque = true
				return
			case codeGSSENCRequest, codeCancelRequest:
			default:
				f.started = true
			}
			continue
		}
		if len(f.buf) < 5 {
			return
		}
		n := int(binary.BigEndian.Uint32(f.buf[1:5]))
		if len(f.buf) < 1+n {
			return
		}
		f.handle(f.buf[0], f.buf[5:1+n])
		f.buf = f.buf[1+n:]
	}
}

func (f *frontend) handle(typ byte, body []byte) {
	switch typ {
	case 'Q':
		sql, _, _ := cstring(body)
		f.emit(Statement{Conn: f.conn, SQL: sql})
	case 'P':
		name, rest, err := cstring(body)
		if err != nil {
			return
		}
		sql, _, err := cstring(rest)
		if err != nil {
			return
		}
		f.stmts[name] = sql
	case 'B':
		_, rest, err := cstring(body) // portal
		if err != nil {
			return
		}
		name, rest, err := cstring(rest)
		if err != nil {
			return
		}
		params, _ := bindParams(rest)
		f.emit(Statement{Conn: f.conn, SQL: f.stmts[name], Params: params})
	}
}

var errShort = errors.New("netsim: short message")

func cstring(b []byte) (string, []byte, error) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), b[i+1:], nil
		}
	}
	return "", nil, errShort
}

// bindParams reads a Bind message's parameter values (after the portal and
// statement names).
func bindParams(b []byte) ([][]byte, error) {
	if len(b) < 2 {
		return nil, errShort
	}
	nf := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < 2*nf+2 {
		return nil, errShort
	}
	b = b[2*nf:]
	np := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	out := make([][]byte, 0, np)
	for range np {
		if len(b) < 4 {
			return out, errShort
		}
		n := int(int32(binary.BigEndian.Uint32(b)))
		b = b[4:]
		if n < 0 {
			out = append(out, nil)
			continue
		}
		if len(b) < n {
			return out, errShort
		}
		out = append(out, append([]byte(nil), b[:n]...))
		b = b[n:]
	}
	return out, nil
}
