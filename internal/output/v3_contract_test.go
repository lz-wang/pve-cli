package output

import (
	"bytes"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestOperationJSONYAMLContracts(t *testing.T) {
	for _, status := range []string{"submitted", "succeeded", "failed", "unknown", "cancel_requested"} {
		r := OperationResult{Resource: "vm", VMID: 100, Node: "n", Task: "UPID:recoverable", Status: status}
		for _, format := range []string{"json", "yaml"} {
			var b bytes.Buffer
			if e := WriteOperation(&b, format, r); e != nil {
				t.Fatal(e)
			}
			var decoded OperationResult
			var e error
			if format == "json" {
				e = json.Unmarshal(b.Bytes(), &decoded)
			} else {
				e = yaml.Unmarshal(b.Bytes(), &decoded)
			}
			if e != nil || decoded != r {
				t.Fatalf("%s %+v %v", format, decoded, e)
			}
		}
	}
}
