package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

const sshKeepaliveInterval = 30 * time.Second

func containsPort(host string) bool {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return true
		}
		if host[i] == ']' {
			return false
		}
	}
	return false
}

func normalizeHostAddr(host string) string {
	if !containsPort(host) {
		return host + ":22"
	}
	return host
}

func buildSSHConfig(user, password string, privateKey []byte) (*ssh.ClientConfig, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	if password != "" {
		cfg.Auth = append(cfg.Auth, ssh.Password(password))
	}

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		cfg.Auth = append(cfg.Auth, ssh.PublicKeys(signer))
	}

	return cfg, nil
}

func dialSSH(host, user, password string, privateKey []byte) (*ssh.Client, error) {
	cfg, err := buildSSHConfig(user, password, privateKey)
	if err != nil {
		return nil, err
	}

	addr := normalizeHostAddr(host)
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: sshKeepaliveInterval,
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server: %w", err)
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to establish SSH connection: %w", err)
	}

	return ssh.NewClient(clientConn, chans, reqs), nil
}

// startSSHKeepalive sends OpenSSH-compatible keepalive requests until stop is closed.
func startSSHKeepalive(sshConn *ssh.Client, stop <-chan struct{}, onFailed func(reason string)) {
	ticker := time.NewTicker(sshKeepaliveInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _, err := sshConn.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					log.Printf("SSH keepalive failed: %v", err)
					onFailed("ssh keepalive failed")
					return
				}
			}
		}
	}()
}
