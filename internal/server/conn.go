package server

import (
	"context"
	"net"
	"sync"

	"google.golang.org/grpc/peer"
)

type ConnTracker struct {
	m sync.Map
}

type trackedConn struct {
	net.Conn
	tracker *ConnTracker
	key     string
}

func (t *trackedConn) Close() error {
	t.tracker.m.Delete(t.key)
	return t.Conn.Close()
}

type trackListener struct {
	net.Listener
	tracker *ConnTracker
}

func (t *ConnTracker) Wrap(lis net.Listener) net.Listener {
	return &trackListener{Listener: lis, tracker: t}
}

func (l *trackListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	key := c.RemoteAddr().String()
	tc := &trackedConn{Conn: c, tracker: l.tracker, key: key}
	l.tracker.m.Store(key, tc)
	return tc, nil
}

func (t *ConnTracker) ClosePeer(ctx context.Context) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return
	}
	if v, ok := t.m.Load(p.Addr.String()); ok {
		_ = v.(net.Conn).Close()
	}
}
