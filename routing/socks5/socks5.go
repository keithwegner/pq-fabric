package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	version5   = byte(0x05)
	cmdConnect = byte(0x01)
	atypIPv4   = byte(0x01)
	atypDomain = byte(0x03)
)

type Dialer interface {
	DialThroughCircuit(ctx context.Context, destination string, payload []byte) ([]byte, error)
}

type DialFunc func(ctx context.Context, destination string, payload []byte) ([]byte, error)

func (f DialFunc) DialThroughCircuit(ctx context.Context, destination string, payload []byte) ([]byte, error) {
	return f(ctx, destination, payload)
}

type Request struct {
	Command     byte
	Destination string
	Payload     []byte
}

type Proxy struct {
	Dialer Dialer
	Allow  func(destination string) bool
}

func (p *Proxy) Handle(ctx context.Context, req Request) ([]byte, error) {
	if req.Command != cmdConnect {
		return nil, fmt.Errorf("unsupported SOCKS5 command 0x%02x", req.Command)
	}
	if req.Destination == "" {
		return nil, errors.New("SOCKS5 destination is required")
	}
	if p.Allow != nil && !p.Allow(req.Destination) {
		return nil, fmt.Errorf("destination %s rejected by local exit policy", req.Destination)
	}
	if p.Dialer == nil {
		return nil, errors.New("SOCKS5 dialer is required")
	}
	return p.Dialer.DialThroughCircuit(ctx, req.Destination, req.Payload)
}

func (p *Proxy) ServeConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	if err := readGreeting(conn); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{version5, 0x00}); err != nil {
		return err
	}
	command, destination, err := readConnectRequest(conn)
	if err != nil {
		_ = writeReply(conn, 0x01)
		return err
	}
	if command != cmdConnect {
		_ = writeReply(conn, 0x07)
		return fmt.Errorf("unsupported SOCKS5 command 0x%02x", command)
	}
	if p.Allow != nil && !p.Allow(destination) {
		_ = writeReply(conn, 0x02)
		return fmt.Errorf("destination %s rejected by local exit policy", destination)
	}
	if err := writeReply(conn, 0x00); err != nil {
		return err
	}
	payload, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	response, err := p.Handle(ctx, Request{Command: command, Destination: destination, Payload: payload})
	if err != nil {
		return err
	}
	_, err = conn.Write(response)
	return err
}

func readGreeting(r io.Reader) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}
	if header[0] != version5 {
		return fmt.Errorf("unsupported SOCKS version 0x%02x", header[0])
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(r, methods); err != nil {
		return err
	}
	return nil
}

func readConnectRequest(r io.Reader) (byte, string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, "", err
	}
	if header[0] != version5 {
		return 0, "", fmt.Errorf("unsupported SOCKS version 0x%02x", header[0])
	}
	command := header[1]
	var host string
	switch header[3] {
	case atypDomain:
		length := make([]byte, 1)
		if _, err := io.ReadFull(r, length); err != nil {
			return 0, "", err
		}
		name := make([]byte, int(length[0]))
		if _, err := io.ReadFull(r, name); err != nil {
			return 0, "", err
		}
		host = string(name)
	case atypIPv4:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return 0, "", err
		}
		host = net.IP(ip).String()
	default:
		return 0, "", fmt.Errorf("unsupported SOCKS address type 0x%02x", header[3])
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return 0, "", err
	}
	port := binary.BigEndian.Uint16(portBytes)
	return command, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func writeReply(w io.Writer, status byte) error {
	_, err := w.Write([]byte{version5, status, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}
