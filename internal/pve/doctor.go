package pve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v2/internal/config"
	"github.com/lz-wang/pve-cli/v2/internal/output"
)

type DoctorOptions struct {
	ConfigPath  string
	ProfileName string
	Offline     bool
	Node        string
	Timeout     time.Duration
	Insecure    bool
	Output      string
	OutputSet   bool
}

type DoctorResult struct {
	Rows   []output.DoctorRow
	Format string
	Failed bool
}

type DoctorService struct {
	backendFactory func(config.Profile, ClientOptions) (Backend, error)
}

func NewDoctorService(backendFactory func(config.Profile, ClientOptions) (Backend, error)) *DoctorService {
	if backendFactory == nil {
		backendFactory = NewProxmoxBackend
	}
	return &DoctorService{backendFactory: backendFactory}
}

func (s *DoctorService) Run(ctx context.Context, options DoctorOptions) DoctorResult {
	result := DoctorResult{Format: output.FormatTable}
	if options.OutputSet {
		result.Format = output.NormalizeFormat(options.Output)
	}

	resolved, err := config.ExpandPath(options.ConfigPath)
	if err != nil {
		result.add("CONFIG_PATH", output.DoctorStatusFail, err.Error())
		result.skipLocalAfter("CONFIG_PATH")
		result.skipOnline(options)
		return result
	}
	result.add("CONFIG_PATH", output.DoctorStatusOK, resolved)

	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.add("CONFIG_FILE", output.DoctorStatusFail, "does not exist")
		} else {
			result.add("CONFIG_FILE", output.DoctorStatusFail, err.Error())
		}
		result.skipLocalAfter("CONFIG_FILE")
		result.skipOnline(options)
		return result
	}
	if info.IsDir() {
		result.add("CONFIG_FILE", output.DoctorStatusFail, "is a directory")
		result.skipLocalAfter("CONFIG_FILE")
		result.skipOnline(options)
		return result
	}
	result.add("CONFIG_FILE", output.DoctorStatusOK, "exists")

	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		result.add("CONFIG_PARSE", output.DoctorStatusFail, err.Error())
		result.skipLocalAfter("CONFIG_PARSE")
		result.skipOnline(options)
		return result
	}
	result.add("CONFIG_PARSE", output.DoctorStatusOK, "yaml parsed")

	profileName, profile, err := cfg.SelectProfile(options.ProfileName)
	if err != nil {
		result.add("CURRENT_PROFILE", output.DoctorStatusFail, err.Error())
		result.skipProfileChecks("CURRENT_PROFILE")
		result.skipOnline(options)
		return result
	}
	result.add("CURRENT_PROFILE", output.DoctorStatusOK, profileName)

	fieldsOK := result.checkProfileFields(profile)
	tokenSecret, tokenOK := result.checkTokenSecret(profile)
	timeout, timeoutOK := result.checkTimeout(profile, options.Timeout)
	defaultOutputOK := result.checkDefaultOutput(profile)
	endpointOK := result.checkEndpoint(profile, options.Insecure)

	if !options.OutputSet && defaultOutputOK && profile.DefaultOutput != "" {
		result.Format = output.NormalizeFormat(profile.DefaultOutput)
	}

	if options.Offline {
		result.skipOnline(options)
		return result
	}

	if !fieldsOK || !tokenOK || !timeoutOK || !endpointOK {
		result.skipOnline(options)
		return result
	}

	backend, err := s.backendFactory(profile, ClientOptions{
		TokenSecret: tokenSecret,
		Timeout:     timeout,
		Insecure:    options.Insecure,
	})
	if err != nil {
		result.add("API_CONNECTIVITY", output.DoctorStatusFail, strings.ReplaceAll(err.Error(), tokenSecret, "[REDACTED]"))
		result.add("NODES", output.DoctorStatusSkip, "skipped due to API_CONNECTIVITY failure")
		result.skipNode(options.Node, "skipped due to API_CONNECTIVITY failure")
		return result
	}

	nodes, err := backend.Nodes(ctx)
	if err != nil {
		result.add("API_CONNECTIVITY", output.DoctorStatusFail, "list nodes: "+strings.ReplaceAll(err.Error(), tokenSecret, "[REDACTED]"))
		result.add("NODES", output.DoctorStatusSkip, "skipped due to API_CONNECTIVITY failure")
		result.skipNode(options.Node, "skipped due to API_CONNECTIVITY failure")
		return result
	}

	result.add("API_CONNECTIVITY", output.DoctorStatusOK, "connected")
	result.add("NODES", output.DoctorStatusOK, fmt.Sprintf("%d node(s)", len(nodes)))
	result.checkNode(options.Node, nodes)
	return result
}

func (r *DoctorResult) add(check string, status output.DoctorStatus, message string) {
	r.Rows = append(r.Rows, output.DoctorRow{
		Check:   check,
		Status:  status,
		Message: message,
	})
	if status == output.DoctorStatusFail {
		r.Failed = true
	}
}

func (r *DoctorResult) skipLocalAfter(check string) {
	checks := []string{
		"CONFIG_FILE",
		"CONFIG_PARSE",
		"CURRENT_PROFILE",
		"PROFILE_FIELDS",
		"TOKEN_SECRET_ENV",
		"TIMEOUT",
		"DEFAULT_OUTPUT",
		"ENDPOINT",
	}
	reason := fmt.Sprintf("skipped due to %s failure", check)
	for _, name := range checks {
		if name == check {
			continue
		}
		r.add(name, output.DoctorStatusSkip, reason)
	}
}

func (r *DoctorResult) skipProfileChecks(check string) {
	reason := fmt.Sprintf("skipped due to %s failure", check)
	for _, name := range []string{"PROFILE_FIELDS", "TOKEN_SECRET_ENV", "TIMEOUT", "DEFAULT_OUTPUT", "ENDPOINT"} {
		r.add(name, output.DoctorStatusSkip, reason)
	}
}

func (r *DoctorResult) skipOnline(options DoctorOptions) {
	reason := "skipped due to previous failure"
	if options.Offline {
		reason = "skipped in offline mode"
	}
	r.add("API_CONNECTIVITY", output.DoctorStatusSkip, reason)
	r.add("NODES", output.DoctorStatusSkip, reason)
	r.skipNode(options.Node, reason)
}

func (r *DoctorResult) skipNode(node, reason string) {
	if node == "" {
		return
	}
	r.add("NODE", output.DoctorStatusSkip, reason)
}

func (r *DoctorResult) checkProfileFields(profile config.Profile) bool {
	var missing []string
	if profile.Endpoint == "" {
		missing = append(missing, "endpoint")
	}
	if profile.TokenID == "" {
		missing = append(missing, "token_id")
	}
	if profile.TokenSecret == "" && profile.TokenSecretEnv == "" {
		missing = append(missing, "token_secret or token_secret_env")
	}
	if len(missing) > 0 {
		r.add("PROFILE_FIELDS", output.DoctorStatusFail, "missing "+strings.Join(missing, ", "))
		return false
	}
	credentialField := "token_secret_env"
	if profile.TokenSecret != "" {
		credentialField = "token_secret"
	}
	r.add("PROFILE_FIELDS", output.DoctorStatusOK, "endpoint, token_id, "+credentialField)
	return true
}

func (r *DoctorResult) checkTokenSecret(profile config.Profile) (string, bool) {
	secret, err := config.ResolveTokenSecret(profile)
	if err != nil {
		r.add("TOKEN_SECRET_ENV", output.DoctorStatusFail, err.Error())
		return "", false
	}
	if profile.TokenSecret != "" {
		r.add("TOKEN_SECRET", output.DoctorStatusOK, "token_secret is configured")
		r.add("TOKEN_SECRET_ENV", output.DoctorStatusSkip, "using token_secret from config")
		return secret, true
	}
	r.add("TOKEN_SECRET_ENV", output.DoctorStatusOK, fmt.Sprintf("%s is set", profile.TokenSecretEnv))
	return secret, true
}

func (r *DoctorResult) checkTimeout(profile config.Profile, override time.Duration) (time.Duration, bool) {
	if override > 0 {
		r.add("TIMEOUT", output.DoctorStatusOK, fmt.Sprintf("%s (from flag)", override))
		return override, true
	}
	if profile.Timeout == "" {
		r.add("TIMEOUT", output.DoctorStatusWarn, "timeout is empty, runtime default 30s will be used")
		return 0, true
	}
	timeout, err := time.ParseDuration(profile.Timeout)
	if err != nil {
		r.add("TIMEOUT", output.DoctorStatusFail, fmt.Sprintf("invalid timeout %q: %v", profile.Timeout, err))
		return 0, false
	}
	r.add("TIMEOUT", output.DoctorStatusOK, profile.Timeout)
	return timeout, true
}

func (r *DoctorResult) checkDefaultOutput(profile config.Profile) bool {
	if profile.DefaultOutput == "" {
		r.add("DEFAULT_OUTPUT", output.DoctorStatusWarn, "default_output is empty, table will be used")
		return false
	}
	format := output.NormalizeFormat(profile.DefaultOutput)
	if err := output.ValidateFormat(format); err != nil {
		r.add("DEFAULT_OUTPUT", output.DoctorStatusFail, err.Error())
		return false
	}
	r.add("DEFAULT_OUTPUT", output.DoctorStatusOK, format)
	return true
}

func (r *DoctorResult) checkEndpoint(profile config.Profile, insecure bool) bool {
	if profile.Endpoint == "" {
		r.add("ENDPOINT", output.DoctorStatusFail, "endpoint is required")
		return false
	}
	parsed, err := url.Parse(profile.Endpoint)
	if err != nil {
		r.add("ENDPOINT", output.DoctorStatusFail, err.Error())
		return false
	}
	if parsed.Scheme == "" {
		r.add("ENDPOINT", output.DoctorStatusFail, "endpoint scheme is required")
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		r.add("ENDPOINT", output.DoctorStatusFail, fmt.Sprintf("unsupported endpoint scheme %q", parsed.Scheme))
		return false
	}
	if parsed.Host == "" {
		r.add("ENDPOINT", output.DoctorStatusFail, "endpoint host is required")
		return false
	}

	var warnings []string
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api2/json") {
		warnings = append(warnings, "path does not end with /api2/json")
	}
	if insecure || profile.InsecureSkipVerify {
		warnings = append(warnings, "TLS verification disabled")
	}
	if len(warnings) > 0 {
		r.add("ENDPOINT", output.DoctorStatusWarn, fmt.Sprintf("%s (%s)", profile.Endpoint, strings.Join(warnings, "; ")))
		return true
	}
	r.add("ENDPOINT", output.DoctorStatusOK, profile.Endpoint)
	return true
}

func (r *DoctorResult) checkNode(node string, nodes []output.NodeRow) {
	if node == "" {
		return
	}
	for _, row := range nodes {
		if row.Name == node {
			r.add("NODE", output.DoctorStatusOK, fmt.Sprintf("node %s exists", node))
			return
		}
	}
	r.add("NODE", output.DoctorStatusFail, fmt.Sprintf("node %s not found", node))
}
