package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cretz/bine/tor"
	ltor "github.com/ipsn/go-libtor"

	"github.com/direct-connect/go-dcpp/hub"
)

const (
	torDir  = ".tor"
	keyFile = "hub.key"
)

func init() {
	hub.RegisterPlugin(&plugin{})
}

type plugin struct {
	h     *hub.Hub
	path  string
	tor   *tor.Tor
	onion net.Listener
}

func (*plugin) Name() string {
	return "Tor"
}

func (*plugin) Version() hub.Version {
	return hub.Version{Major: 0, Minor: 1}
}

func (p *plugin) Init(h *hub.Hub, path string) error {
	p.h = h
	p.path = path
	if err := p.startTor(); err != nil {
		return err
	}
	if err := p.listenAndServe(); err != nil {
		return err
	}
	return nil
}

func (p *plugin) startTor() error {
	fmt.Println("Tor version:", ltor.ProviderVersion())

	torDir := filepath.Join(p.path, torDir)
	if _, err := os.Stat(torDir); os.IsNotExist(err) {
		if err = os.MkdirAll(torDir, 0755); err != nil {
			return fmt.Errorf("tor: failed to create directory: %v", err)
		}
	} else if err != nil {
		return err
	}
	torrc := filepath.Join(torDir, "torrc")
	if _, err := os.Stat(torrc); os.IsNotExist(err) {
		if err = ioutil.WriteFile(torrc, nil, 0644); err != nil {
			return fmt.Errorf("tor: failed to create torrc: %v", err)
		}
	} else if err != nil {
		return err
	}
	t, err := tor.Start(nil, &tor.StartConf{
		DataDir:        torDir,
		TorrcFile:      torrc,
		ProcessCreator: ltor.Creator,
		DebugWriter:    os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("failed to start Tor: %v", err)
	}
	p.tor = t
	return nil
}

func (p *plugin) listenAndServe() error {
	// Wait at most a few minutes to publish the service
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	keyFile := filepath.Join(p.path, torDir, keyFile)
	var key crypto.PrivateKey
	if data, err := ioutil.ReadFile(keyFile); err == nil {
		k, err := x509.ParsePKCS1PrivateKey(data)
		if err != nil {
			return err
		}
		key = k
		fmt.Printf("Loaded Tor private key: %s\n", keyFile)
	} else if !os.IsNotExist(err) {
		return err
	}

	// Create an onion service to listen on any port but show as 80
	const torPort = 1411
	onion, err := p.tor.Listen(ctx, &tor.ListenConf{
		RemotePorts: []int{torPort},
		Key:         key,
	})
	if err != nil {
		return fmt.Errorf("tor: failed to start the service: %v", err)
	}
	if key == nil {
		if k, ok := onion.Key.(*rsa.PrivateKey); ok {
			data := x509.MarshalPKCS1PrivateKey(k)
			err := ioutil.WriteFile(keyFile, data, 0600)
			if err != nil {
				return err
			}
			fmt.Printf("Saved Tor private key: %s\n", keyFile)
		}
	}
	p.onion = onion
	addr := fmt.Sprintf("%v.onion:%d", onion.ID, torPort)
	fmt.Printf("Tor address: %s\n", addr)
	p.h.AddAddress(addr)
	go p.serve(onion)
	return nil
}

func (p *plugin) serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("tor:", err)
			return
		}
		go func() {
			defer conn.Close()
			if err := p.h.Serve(conn); err != nil {
				log.Println("tor:", err)
			}
		}()
	}
}

func (p *plugin) Close() error {
	if p.onion != nil {
		log.Println("tor: stopping service")
		_ = p.onion.Close()
		p.onion = nil
	}
	if p.tor != nil {
		log.Println("tor: stopping node")
		_ = p.tor.Close()
		p.tor = nil
	}
	return nil
}
