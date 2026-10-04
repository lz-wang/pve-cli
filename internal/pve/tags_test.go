package pve

import (
	"reflect"
	"testing"
)

func TestParseGuestTags(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", []string{}},
		{"infra", []string{"infra"}},
		{"infra,production", []string{"infra", "production"}},
		{" infra , production ", []string{"infra", "production"}},
		{"Infra,,PRODUCTION", []string{"infra", "production"}},
	}
	for _, tc := range cases {
		if got := ParseGuestTags(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("ParseGuestTags(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

func TestParseTagMatch(t *testing.T) {
	for _, value := range []string{"", "all", "any", "ANY"} {
		if _, err := ParseTagMatch(value); err != nil {
			t.Fatalf("ParseTagMatch(%q): %v", value, err)
		}
	}
	if _, err := ParseTagMatch("bogus"); err == nil {
		t.Fatal("expected invalid tag match error")
	}
}

func TestMatchGuestTags(t *testing.T) {
	guest := ParseGuestTags("infra,docker")

	cases := []struct {
		wanted []string
		match  string
		want   bool
	}{
		{nil, TagMatchAll, true},
		{[]string{"infra"}, TagMatchAll, true},
		{[]string{"infra", "docker"}, TagMatchAll, true},
		{[]string{"infra", "production"}, TagMatchAll, false},
		{[]string{"infra", "production"}, TagMatchAny, true},
		{[]string{"missing"}, TagMatchAny, false},
		{[]string{"DOCKER"}, TagMatchAny, true},
	}
	for _, tc := range cases {
		if got := MatchGuestTags(guest, tc.wanted, tc.match); got != tc.want {
			t.Fatalf("MatchGuestTags(%v, %v, %q) = %v, want %v", guest, tc.wanted, tc.match, got, tc.want)
		}
	}
}
