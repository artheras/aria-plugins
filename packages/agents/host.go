package agents

import (
	"context"
	"errors"
	"github.com/artheras/aria-plugins/packages/sdk"
)

// Host leaves language understanding to the MCP client. It never simulates AI.
type Host struct{}

func (Host) Name() string { return "Connected agent (structured selections)" }
func (Host) Draft(context.Context, string, []sdk.Product) (Draft, error) {
	return Draft{}, errors.New("use commerce_draft_order with items from your connected agent; server-side text drafting is disabled")
}
