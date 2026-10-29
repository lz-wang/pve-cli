package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"net/url"
)

type NodePowerBackend interface {
	PowerNode(context.Context, string, string) error
}

func (b *ProxmoxBackend) PowerNode(ctx context.Context, node, action string) error {
	if action != "reboot" && action != "shutdown" {
		return fmt.Errorf("%w: invalid node power action", ErrInvalid)
	}
	return ClassifyError(b.client.Post(ctx, "/nodes/"+url.PathEscape(node)+"/status", map[string]string{"command": action}, nil))
}

type NodePowerPlan struct {
	Node       string
	Action     string
	RunningVM  int
	RunningLXC int
}

func (s *NodeService) PlanPower(ctx context.Context, node, action string) (NodePowerPlan, error) {
	plan := NodePowerPlan{Node: node, Action: action}
	if !resourceNamePattern.MatchString(node) || (action != "reboot" && action != "shutdown") {
		return plan, fmt.Errorf("%w: node and reboot/shutdown action required", ErrInvalid)
	}
	row, err := s.backend.Node(ctx, node)
	if err != nil {
		return plan, ClassifyError(err)
	}
	if row.Status != "online" {
		return plan, fmt.Errorf("%w: node is not online", ErrUnavailable)
	}
	b, ok := s.backend.(GuestBackend)
	if !ok {
		return plan, fmt.Errorf("%w: guest inventory unavailable", ErrUnavailable)
	}
	guests, err := NewGuestAggregateService(b, nil, false).List(ctx, GuestListOptions{Node: node, Status: "running", RequireComplete: true})
	if err != nil {
		return plan, ClassifyError(err)
	}
	for _, g := range guests {
		if g.Kind == "vm" {
			plan.RunningVM++
		} else {
			plan.RunningLXC++
		}
	}
	return plan, nil
}
func (s *NodeService) Power(ctx context.Context, node, action string) (output.OperationResult, error) {
	result := output.OperationResult{Resource: "node", Node: node, Status: "unknown"}
	if _, err := s.PlanPower(ctx, node, action); err != nil {
		return result, err
	}
	b, ok := s.backend.(NodePowerBackend)
	if !ok {
		return result, fmt.Errorf("%w: node power unavailable", ErrUnavailable)
	}
	if err := b.PowerNode(ctx, node, action); err != nil {
		return result, err
	}
	result.Status = "submitted"
	return result, nil
}
