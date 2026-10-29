package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ReadCredential reads a private regular file without logging its contents.
func ReadCredential(path string) (string, error) {
	path, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("read credential file: %w", err)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) || !credentialOwnedByUser(info) {
		return "", fmt.Errorf("credential file must be a private regular file (0600)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read credential file: %w", err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("credential file is empty")
	}
	return secret, nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pve-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // best-effort cleanup of an unpublished temporary file
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// StoreCredential writes a new immutable credential before publishing its path.
func StoreCredential(configPath, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("token secret is empty")
	}
	path, err := ExpandPath(configPath)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(path), "credentials")
	if info, e := os.Lstat(dir); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !credentialOwnedByUser(info)) {
		return "", fmt.Errorf("credential directory must be owned by the current user and cannot be a symlink")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "token-*")
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(f.Name())
	if err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = atomicWrite(path, []byte(secret)); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
