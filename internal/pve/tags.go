package pve

import (
	"fmt"
	"strings"
)

// Tag match modes for guest tag filters.
const (
	TagMatchAll = "all"
	TagMatchAny = "any"
)

// ParseGuestTags splits a raw PVE tags string into normalized tag values.
// Proxmox VE (and go-proxmox's TagSeperator) joins guest tags with ";", so
// that is the canonical separator. This is the single place raw tag strings
// are decoded.
func ParseGuestTags(raw string) []string {
	parts := strings.Split(raw, ";")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		tag := strings.ToLower(strings.TrimSpace(part))
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

// ParseTagMatch validates the --tag-match value.
func ParseTagMatch(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TagMatchAll:
		return TagMatchAll, nil
	case TagMatchAny:
		return TagMatchAny, nil
	default:
		return "", fmt.Errorf("invalid tag match %q, expected all or any", value)
	}
}

// MatchGuestTags reports whether the guest tag set matches the wanted tags.
// An empty wanted set matches everything.
func MatchGuestTags(guestTags []string, wanted []string, match string) bool {
	if len(wanted) == 0 {
		return true
	}

	normalized := make(map[string]struct{}, len(guestTags))
	for _, tag := range guestTags {
		normalized[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}

	matched := 0
	for _, want := range wanted {
		if _, ok := normalized[strings.ToLower(strings.TrimSpace(want))]; ok {
			matched++
		}
	}
	if match == TagMatchAny {
		return matched > 0
	}
	return matched == len(wanted)
}
