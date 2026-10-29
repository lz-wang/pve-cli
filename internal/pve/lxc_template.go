package pve

import (
	"context"
	"fmt"
	proxmox "github.com/luthermonson/go-proxmox"
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
	// The SDK appliance model omits size, so retain it with a narrow response wrapper.
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

// TemplateDownloader submits exactly one appliance download request.
type TemplateDownloader interface {
	DownloadTemplate(context.Context, string, string, string) (Task, error)
}

func (b *ProxmoxBackend) DownloadTemplate(ctx context.Context, node, storage, name string) (Task, error) {
	nodeAPI := (&proxmox.Node{}).New(b.client, node)
	upid, err := nodeAPI.DownloadAppliance(ctx, name, storage)
	if err != nil {
		return nil, ClassifyError(err)
	}
	task, err := b.TaskHandle(upid)
	if err != nil {
		return nil, fmt.Errorf("%w: download accepted but invalid UPID returned", ErrTaskTimeout)
	}
	return task, nil
}
func (s *TemplateService) Download(ctx context.Context, node, storage, name string) (output.OperationResult, error) {
	result := output.OperationResult{Resource: "lxc-template", Node: node, Status: "unknown"}
	if node == "" || storage == "" || name == "" || path.Base(name) != name {
		return result, fmt.Errorf("%w: node, storage and exact template filename required", ErrInvalid)
	}
	available, err := s.List(ctx, TemplateOptions{Node: node, Available: true})
	if err != nil {
		return result, err
	}
	exists := false
	for _, r := range available {
		if r.Name == name {
			exists = true
		}
	}
	if !exists {
		return result, fmt.Errorf("%w: template %s", ErrNotFound, name)
	}
	target, err := s.backend.Storage(ctx, node, storage)
	if err != nil {
		return result, ClassifyError(err)
	}
	if !storageSupports(target, "vztmpl") {
		return result, fmt.Errorf("%w: storage does not support active vztmpl content", ErrInvalid)
	}
	local, err := s.List(ctx, TemplateOptions{Node: node, Storage: storage})
	if err != nil {
		return result, err
	}
	for _, r := range local {
		if r.Name == name {
			return result, fmt.Errorf("%w: template already downloaded", ErrConflict)
		}
	}
	backend, ok := s.backend.(TemplateDownloader)
	if !ok {
		return result, fmt.Errorf("%w: template download unavailable", ErrUnavailable)
	}
	task, err := backend.DownloadTemplate(ctx, node, storage, name)
	if err != nil {
		return result, err
	}
	runner := s.tasks
	runner.Resource = result.Resource
	runner.Node = node
	result, err = runner.Execute(ctx, task)
	if err != nil {
		return result, err
	}
	if result.Status == "succeeded" {
		contents, e := s.List(ctx, TemplateOptions{Node: node, Storage: storage})
		if e == nil {
			for _, r := range contents {
				if r.Name == name {
					return result, nil
				}
			}
		}
		result.Status = "unknown"
		result.Error = "download completed but storage content could not be verified"
		return result, fmt.Errorf("%w: %s; task %s", ErrTaskTimeout, result.Error, result.Task)
	}
	return result, nil
}
func storageSupports(row output.StorageRow, content string) bool {
	if !row.Active || !row.Enabled {
		return false
	}
	for _, kind := range strings.Split(row.Content, ",") {
		if strings.TrimSpace(kind) == content {
			return true
		}
	}
	return false
}
