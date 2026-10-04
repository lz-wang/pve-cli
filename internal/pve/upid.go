package pve

import (
	"fmt"
	"strconv"
	"strings"
)

// UPIDInfo carries the fields embedded in a Proxmox task UPID:
// UPID:<node>:<pid>:<pstart>:<starttime>:<type>:<id>:<user>
type UPIDInfo struct {
	UPID      string
	Node      string
	Type      string
	ID        string
	User      string
	StartTime int64
}

// ParseUPID is the single place where UPID strings are decoded. CLI and
// service layers must not split UPIDs manually.
func ParseUPID(upid string) (UPIDInfo, error) {
	upid = strings.TrimSpace(upid)
	if upid == "" {
		return UPIDInfo{}, fmt.Errorf("upid is required")
	}
	parts := strings.Split(upid, ":")
	if len(parts) < 8 || parts[0] != "UPID" {
		return UPIDInfo{}, fmt.Errorf("invalid upid %q", upid)
	}

	info := UPIDInfo{
		UPID: upid,
		Node: parts[1],
		Type: parts[5],
		ID:   parts[6],
		User: parts[7],
	}
	if info.Node == "" {
		return UPIDInfo{}, fmt.Errorf("invalid upid %q: missing node", upid)
	}
	if parts[4] != "" {
		if seconds, err := strconv.ParseInt(parts[4], 16, 64); err == nil {
			info.StartTime = seconds
		}
	}
	return info, nil
}
