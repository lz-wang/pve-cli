package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"net/url"
	"path"
	"sort"
	"strings"
)

// AvailableTemplateReader reads the node's appliance index.
type AvailableTemplateReader interface {
	AvailableTemplates(context.Context, string) ([]output.TemplateRow, error)
}

func (b *ProxmoxBackend) AvailableTemplates(ctx context.Context, node string) ([]output.TemplateRow, error) {
	var templates []struct {
		Template string `json:"template"`
		Section  string `json:"section"`
		Size     uint64 `json:"size"`
	}
	if err := b.client.Get(ctx, "/nodes/"+url.PathEscape(node)+"/aplinfo", &templates); err != nil {
		return nil, ClassifyError(err)
	}
	rows := make([]output.TemplateRow, 0, len(templates))
	for _, r := range templates {
		rows = append(rows, output.TemplateRow{Name: r.Template, Section: r.Section, Size: r.Size, Node: node, Available: true})
	}
	return rows, nil
}

type TemplateOptions struct {
	Node, Storage, Section, Name string
	Available                    bool
}
type TemplateService struct {
	backend StorageBackend
	tasks   TaskRunner
}

func NewTemplateService(backend StorageBackend, tasks TaskRunner) *TemplateService {
	return &TemplateService{backend: backend, tasks: tasks}
}
func (s *TemplateService) List(ctx context.Context, o TemplateOptions) ([]output.TemplateRow, error) {
	if strings.TrimSpace(o.Node) == "" {
		return nil, fmt.Errorf("%w: node is required", ErrInvalid)
	}
	if o.Section != "" && o.Section != "system" {
		return nil, fmt.Errorf("%w: only system container templates are supported", ErrInvalid)
	}
	rows := []output.TemplateRow{}
	if o.Available {
		backend, ok := s.backend.(AvailableTemplateReader)
		if !ok {
			return nil, fmt.Errorf("%w: appliance index unavailable", ErrUnavailable)
		}
		var err error
		rows, err = backend.AvailableTemplates(ctx, o.Node)
		if err != nil {
			return nil, err
		}
	} else {
		if o.Storage == "" {
			return nil, fmt.Errorf("%w: storage is required for downloaded templates", ErrInvalid)
		}
		contents, err := s.backend.StorageContents(ctx, o.Node, o.Storage)
		if err != nil {
			return nil, ClassifyError(err)
		}
		for _, r := range contents {
			if r.Content != "vztmpl" {
				continue
			}
			rows = append(rows, output.TemplateRow{Name: path.Base(r.VolID), Section: "system", Size: r.Size, Node: o.Node, Storage: o.Storage, VolID: r.VolID})
		}
	}
	result := make([]output.TemplateRow, 0, len(rows))
	for _, r := range rows {
		if r.Section != "system" {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Name), strings.ToLower(o.Name)) {
			continue
		}
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
