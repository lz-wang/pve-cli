package pve

import (
	"context"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

type templateFake struct {
	StorageBackend
	rows []output.TemplateRow
}

func (b templateFake) AvailableTemplates(context.Context, string) ([]output.TemplateRow, error) {
	return b.rows, nil
}
func TestTemplateModes(t *testing.T) {
	s := NewTemplateService(templateFake{rows: []output.TemplateRow{{Name: "debian.tar.zst", Section: "system", Available: true}, {Name: "app", Section: "turnkeylinux"}}}, TaskRunner{})
	rows, e := s.List(context.Background(), TemplateOptions{Node: "n", Available: true, Name: "DEBIAN"})
	if e != nil || len(rows) != 1 || !rows[0].Available || rows[0].VolID != "" {
		t.Fatalf("%+v %v", rows, e)
	}
	if _, e = s.List(context.Background(), TemplateOptions{Node: "n"}); e == nil {
		t.Fatal("missing storage accepted")
	}
}
