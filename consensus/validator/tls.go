package validator

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/keithwegner/pq-fabric/core/consortium"
)

type peerTLSConfig struct {
	Server *tls.Config
	Client *tls.Config
}

func (c *peerTLSConfig) Listener(listener net.Listener) net.Listener {
	return tls.NewListener(listener, c.Server)
}

func loadPeerTLS(cfg Config) (*peerTLSConfig, *http.Client, error) {
	if cfg.PeerTLSCertFile == "" && cfg.PeerTLSKeyFile == "" && cfg.PeerTLSCAFile == "" {
		return nil, nil, nil
	}
	if cfg.PeerTLSCertFile == "" || cfg.PeerTLSKeyFile == "" || cfg.PeerTLSCAFile == "" {
		return nil, nil, errors.New("peer mTLS requires cert, key, and CA files")
	}
	cert, err := tls.LoadX509KeyPair(cfg.PeerTLSCertFile, cfg.PeerTLSKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load peer TLS certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.PeerTLSCAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read peer TLS CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, nil, errors.New("peer TLS CA file contains no PEM certificates")
	}
	server := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}
	client := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}
	return &peerTLSConfig{Server: server, Client: client}, &http.Client{Transport: &http.Transport{TLSClientConfig: client}}, nil
}

func (n *Node) withPeerAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !n.cfg.ProductionMode {
			handler(w, r)
			return
		}
		if _, err := n.authorizePeer(r); err != nil {
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		handler(w, r)
	}
}

func (n *Node) authorizePeer(r *http.Request) (string, error) {
	if r.TLS == nil {
		return "", errors.New("peer mTLS is required")
	}
	if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return "", errors.New("verified peer client certificate is required")
	}
	if n.manifest == nil {
		return "", errors.New("consortium manifest is required for peer mTLS")
	}
	peerID, err := consortium.ValidatorIDFromTLSURIs(r.TLS.PeerCertificates[0].URIs, n.manifest.ConsortiumID)
	if err != nil {
		return "", err
	}
	if !containsString(n.validatorIDs, peerID) {
		return "", fmt.Errorf("peer certificate validator %s is not active in current consortium manifest", peerID)
	}
	return peerID, nil
}
