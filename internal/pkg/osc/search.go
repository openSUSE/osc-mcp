package osc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcPkgParam struct {
	Name string `json:"name" jsonschema:"Name of the source package to search"`
}

func (cred OSCCredentials) SearchSrcPkg(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchSrcPkgParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	jsonByte, err := json.Marshal(cred)
	if err != nil {
		return nil, fmt.Errorf("error on qery, couldn't marshall credentials")
	}
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonByte),
			},
		},
	}, nil
}
