package output

// ProfileReport wraps opt-in queries without changing single-profile contracts.
type ProfileReport struct {
	Complete bool            `json:"complete" yaml:"complete"`
	Results  []ProfileResult `json:"results" yaml:"results"`
}
type ProfileResult struct {
	Profile  string `json:"profile" yaml:"profile"`
	Complete bool   `json:"complete" yaml:"complete"`
	Data     any    `json:"data" yaml:"data"`
	Error    string `json:"error,omitempty" yaml:"error,omitempty"`
}
