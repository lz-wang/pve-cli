//go:build !darwin && !linux

package config

import "os"

// Unix ownership bits are unavailable here; platform ACLs control access.
func credentialOwnedByUser(os.FileInfo) bool { return true }
