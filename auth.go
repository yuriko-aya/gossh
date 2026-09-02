package main

import (
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/fernet/fernet-go"
)

func decryptAccess(encrypted string) (SSHCredentials, error) {
	var creds SSHCredentials

	fernetKey := getDefaultFernetKey()
	if fernetKey == "" {
		return creds, fmt.Errorf("fernet key not configured")
	}

	key, err := fernet.DecodeKeys(fernetKey)
	if err != nil {
		return creds, fmt.Errorf("invalid fernet key: %v", err)
	}

	token64 := fernet.VerifyAndDecrypt([]byte(encrypted), tokenTTL(), key)
	if token64 == nil {
		return creds, fmt.Errorf("failed to decrypt access token")
	}

	token, err := base64.StdEncoding.DecodeString(string(token64))
	if err != nil {
		return creds, err
	}

	values, err := url.ParseQuery(string(token))
	if err != nil {
		return creds, err
	}

	creds.User = values.Get("username")
	creds.Host = values.Get("hostname")
	creds.Password = values.Get("password")
	creds.PrivateKey = values.Get("privatekey")

	return creds, nil
}

func encryptAccess(creds SSHCredentials) (string, error) {
	fernetKey := getDefaultFernetKey()
	if fernetKey == "" {
		return "", fmt.Errorf("fernet key not configured")
	}
	keys, err := fernet.DecodeKeys(fernetKey)
	if err != nil {
		return "", fmt.Errorf("invalid fernet key: %v", err)
	}

	values := url.Values{}
	values.Set("username", creds.User)
	values.Set("hostname", creds.Host)
	if creds.Password != "" {
		values.Set("password", creds.Password)
	}
	if creds.PrivateKey != "" {
		values.Set("privatekey", creds.PrivateKey)
	}

	dataB64 := base64.StdEncoding.EncodeToString([]byte(values.Encode()))
	tok, err := fernet.EncryptAndSign([]byte(dataB64), keys[0])
	if err != nil {
		return "", fmt.Errorf("encryption failed: %v", err)
	}
	return string(tok), nil
}
