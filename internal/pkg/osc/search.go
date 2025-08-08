package osc

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcPkgParam struct {
	Name string `json:"name" jsonschema:"Name of the source package to search"`
}

func (cred OSCCredentials) SearchSrcPkg(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchSrcPkgParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	return
}
