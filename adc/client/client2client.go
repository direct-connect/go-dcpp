package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/dennwc/go-dcpp/adc"
)

var (
	ErrPeerOffline = errors.New("peer is offline")
	ErrPeerPassive = errors.New("peer is passive")
)

func (p *Peer) CanDial() bool {
	// online and active
	return p.Online() && p.Info().Features.Has(adc.FeaTCP4)
}

// Dial tries to dial the peer either in passive or active mode.
func (p *Peer) Dial(ctx context.Context) (*PeerConn, error) {
	if !p.Online() {
		return nil, ErrPeerOffline
	}
	// TODO: active mode
	return p.dialPassive(ctx)
}

func (p *Peer) dialPassive(ctx context.Context) (*PeerConn, error) {
	if !p.Info().Features.Has(adc.FeaTCP4) {
		return nil, ErrPeerPassive
	}
	sid := p.getSID()
	if sid == nil {
		return nil, ErrPeerOffline
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	token, caddr, errc := p.hub.revConnToken(ctx, p.Info().Id)

	err := p.hub.writeDirect(*sid, adc.RevConnectRequest{
		Proto: adc.ProtoADC, Token: token,
	})
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err = <-errc:
		return nil, err
	case addr := <-caddr:
		pconn, err := adc.Dial(addr)
		if err != nil {
			return nil, err
		}
		fea, err := p.handshakePassive(pconn, token)
		if err != nil {
			pconn.Close()
			return nil, err
		}
		return &PeerConn{p: p, conn: pconn, fea: fea}, nil
	}
}

func (p *Peer) clientExtensions() adc.ModFeatures {
	return adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaBAS0: true,
		adc.FeaTIGR: true,
		// extensions
		adc.FeaBZIP: true,
	}
}

func (p *Peer) handshakePassive(conn *adc.Conn, token string) (adc.ModFeatures, error) {
	// we are dialing - send things upfront

	// send our features
	ourFeatures := p.clientExtensions()
	err := conn.WriteClientMsg(adc.Supported{
		Features: ourFeatures,
	})

	// send an identification as well
	err = conn.WriteClientMsg(adc.User{
		Id: p.hub.CID(), Token: token,
	})

	// flush both
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(time.Second * 5)
	// wait for a list of features
	msg, err := conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	sup, ok := msg.(adc.Supported)
	if !ok {
		return nil, fmt.Errorf("expected a list of peer's features, got: %#v", msg)
	}
	peerFeatures := sup.Features
	if !peerFeatures.IsSet(adc.FeaBASE) || !peerFeatures.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("no basic features support for peer: %v", peerFeatures)
	}

	// wait for an identification
	msg, err = conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	u, ok := msg.(adc.User)
	if !ok {
		return nil, fmt.Errorf("expected a peer's identity, got: %#v", msg)
	} else if u.Id != p.Info().Id {
		return nil, fmt.Errorf("wrong client connected: %v", u.Id)
	}
	return ourFeatures.Intersect(peerFeatures), nil
}

func (p *Peer) handshakeActive(conn *adc.Conn, token string) (adc.ModFeatures, error) {
	// we are accepting the connection, so wait for a message from peer
	deadline := time.Now().Add(time.Second * 5)

	// wait for a list of features
	msg, err := conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	sup, ok := msg.(adc.Supported)
	if !ok {
		return nil, fmt.Errorf("expected a list of peer's features, got: %#v", msg)
	}
	peerFeatures := sup.Features
	if !peerFeatures.IsSet(adc.FeaBASE) || !peerFeatures.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("no basic features support for peer: %v", peerFeatures)
	}

	// send our features
	ourFeatures := p.clientExtensions()
	err = conn.WriteClientMsg(adc.Supported{
		Features: ourFeatures,
	})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	// wait for an identification
	msg, err = conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	u, ok := msg.(adc.User)
	if !ok {
		return nil, fmt.Errorf("expected a peer's identity, got: %#v", msg)
	} else if u.Id != p.Info().Id {
		return nil, fmt.Errorf("wrong client connected: %v", u.Id)
	} else if u.Token != token {
		return nil, errors.New("wrong auth token")
	}

	// identify ourselves
	err = conn.WriteClientMsg(adc.User{
		Id: p.hub.CID(),
	})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	return ourFeatures.Intersect(peerFeatures), nil
}

type PeerConn struct {
	p   *Peer
	fea adc.ModFeatures

	mu   sync.Mutex
	conn *adc.Conn
}

func (c *PeerConn) Close() error {
	return c.conn.Close()
}

func (c *PeerConn) Peer() *Peer {
	return c.p
}

type FileInfo struct {
	Size int64
	TTH  adc.TTH
}

type File interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	Size() int64
	TTH() adc.TTH
}

func (c *PeerConn) statFile(ctx context.Context, typ, path string) (*adc.SearchResult, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 3)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.WriteClientMsg(adc.GetInfoRequest{
		Type: typ, Path: path,
	})
	if err != nil {
		return nil, err
	}
	err = c.conn.Flush()
	if err != nil {
		return nil, err
	}
	msg, err := c.conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	res, ok := msg.(adc.SearchResult)
	if !ok {
		return nil, fmt.Errorf("expected file info (search) response, got: %#v", msg)
	}
	return &res, nil
}

func (c *PeerConn) readFile(ctx context.Context, typ, path string, off, size int64) (io.ReadCloser, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 5)
	}
	if size <= 0 {
		size = -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.WriteClientMsg(adc.GetRequest{
		Type: typ, Path: path,
		Start: off, Bytes: size,
	})
	if err != nil {
		return nil, err
	}
	err = c.conn.Flush()
	if err != nil {
		return nil, err
	}
	msg, err := c.conn.ReadClientMsg(deadline)
	if err != nil {
		return nil, err
	}
	res, ok := msg.(adc.GetResponse)
	if !ok {
		return nil, fmt.Errorf("expected file info (search) response, got: %#v", msg)
	} else if res.Start != off {
		c.Close()
		return nil, fmt.Errorf("requested %d offset, got %d", off, res.Start)
	} else if res.Type != typ {
		c.Close()
		return nil, fmt.Errorf("requested %q type, got %q", typ, res.Type)
	} else if res.Path != path {
		c.Close()
		return nil, fmt.Errorf("requested %q path, got %q", path, res.Path)
	}
	// it's safe to unlock the mutex here since the underlying conn
	// will be blocked and other requests will wait for this one complete
	// TODO: open a new connection if we get another read request
	return c.conn.ReadBinary(res.Bytes), nil
}

func (c *PeerConn) StatFile(ctx context.Context, path string) (FileInfo, error) {
	info, err := c.statFile(ctx, "file", path)
	if err != nil {
		// TODO: handle not found error
		return FileInfo{}, err
	}
	return FileInfo{Size: info.Size, TTH: info.Tiger}, nil
}

func (c *PeerConn) GetFile(ctx context.Context, path string) (File, error) {
	info, err := c.statFile(ctx, "file", path)
	if err != nil {
		// TODO: handle not found error
		return nil, err
	}
	return &peerFile{c: c, typ: "file", path: path, info: *info}, nil
}

func (c *PeerConn) StatFileList(ctx context.Context, path string) (FileInfo, error) {
	if !c.fea.IsSet(adc.FeaBZIP) {
		// TODO: implement if it happens in the wild
		return FileInfo{}, fmt.Errorf("bzip file list is not supported by peer")
	}
	return c.StatFile(ctx, adc.FileListBZIP)
}

func (c *PeerConn) GetFileListBZIP(ctx context.Context) (io.ReadCloser, error) {
	if !c.fea.IsSet(adc.FeaBZIP) {
		// TODO: implement if it happens in the wild
		return nil, fmt.Errorf("bzip file list is not supported by peer")
	}
	return c.readFile(ctx, "file", adc.FileListBZIP, 0, -1)
}

type peerFile struct {
	c    *PeerConn
	typ  string
	path string
	info adc.SearchResult

	off int64
	r   io.ReadCloser
}

func (f *peerFile) cancel() {
	if f.r != nil {
		// TODO: it will try to drain the whole file; check if we can cancel it properly
		_ = f.r.Close()
		f.r = nil
	}
}

func (f *peerFile) openFile() error {
	if f.r != nil {
		return nil
	}
	rc, err := f.c.readFile(context.Background(), f.typ, f.path, f.off, -1)
	if err != nil {
		return err
	}
	f.r = rc
	return nil
}

func (f *peerFile) Read(p []byte) (int, error) {
	if f.off >= f.info.Size {
		return 0, io.EOF
	}
	if err := f.openFile(); err != nil {
		return 0, err
	}
	n, err := f.r.Read(p)
	if err == io.EOF {
		_ = f.r.Close()
		f.r = nil
	}
	f.off += int64(n)
	return n, err
}

func (f *peerFile) ReadAt(p []byte, off int64) (int, error) {
	if d := f.info.Size - off; d <= 0 {
		return 0, io.EOF
	} else if len(p) > int(d) {
		p = p[:d]
	}
	// make sure we close the previous transfer
	f.cancel()
	rc, err := f.c.readFile(context.Background(), f.typ, f.path, off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	// read full buffer, since we adjusted its length at the beginning
	return io.ReadFull(rc, p)
}

func (f *peerFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		// same
	case io.SeekCurrent:
		offset += f.off
	case io.SeekEnd:
		offset = f.info.Size - offset
	default:
		return 0, fmt.Errorf("wrong whence: %v", whence)
	}
	if f.off != offset && f.r != nil {
		f.cancel()
	}
	f.off = offset
	return offset, nil
}

func (f *peerFile) Close() error {
	if f.r == nil {
		return nil
	}
	err := f.r.Close()
	f.r = nil
	return err
}

func (f *peerFile) Size() int64 {
	return f.info.Size
}

func (f *peerFile) TTH() adc.TTH {
	return f.info.Tiger
}
