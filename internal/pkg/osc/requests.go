package osc

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListRequestsCmd struct {
	User            string `json:"user,omitempty" jsonschema:"Username to get requests for. If not provided, it will use the configured user."`
	Group           string `json:"group,omitempty" jsonschema:"Group name to filter requests."`
	Project         string `json:"project,omitempty" jsonschema:"Project name to filter requests."`
	Package         string `json:"package,omitempty" jsonschema:"Package name to filter requests."`
	States          string `json:"states,omitempty" jsonschema:"Comma-separated list of request states (e.g., 'new,review'). Defaults to 'new,review'"`
	ReviewStates    string `json:"reviewstates,omitempty" jsonschema:"Comma-separated list of review states."`
	Types           string `json:"types,omitempty" jsonschema:"Comma-separated list of action types."`
	Roles           string `json:"roles,omitempty" jsonschema:"Comma-separated list of roles."`
	WithHistory     bool   `json:"withhistory,omitempty" jsonschema:"Include history of request."`
	WithFullHistory bool   `json:"withfullhistory,omitempty" jsonschema:"Include full history of request with all comments."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Limit number of requests."`
}

type RequestCollection struct {
	XMLName  xml.Name       `xml:"collection"`
	Matches  string         `xml:"matches,attr"`
	Requests []ShortRequest `xml:"request"`
}

type ShortRequest struct {
	XMLName     xml.Name        `xml:"request"`
	ID          string          `xml:"id,attr"`
	Creator     string          `xml:"creator,attr"`
	Created     string          `xml:"created,attr"`
	Actions     []RequestAction `xml:"action"`
	State       RequestState    `xml:"state"`
	Description string          `xml:"description"`
}

type RequestAction struct {
	XMLName xml.Name        `xml:"action"`
	Type    string          `xml:"type,attr"`
	Source  RequestSource   `xml:"source"`
	Target  RequestTarget   `xml:"target"`
	Persons []RequestPerson `xml:"person"`
	Groups  []RequestGroup  `xml:"group"`
}

type RequestSource struct {
	XMLName xml.Name `xml:"source"`
	Project string   `xml:"project,attr"`
	Package string   `xml:"package,attr"`
	Rev     string   `xml:"rev,attr"`
}

type RequestTarget struct {
	XMLName xml.Name `xml:"target"`
	Project string   `xml:"project,attr"`
	Package string   `xml:"package,attr"`
}

type RequestPerson struct {
	XMLName xml.Name `xml:"person"`
	Name    string   `xml:"name,attr"`
	Role    string   `xml:"role,attr"`
}

type RequestGroup struct {
	XMLName xml.Name `xml:"group"`
	Name    string   `xml:"name,attr"`
	Role    string   `xml:"role,attr"`
}

type RequestState struct {
	Name       string `xml:"name,attr"`
	Who        string `xml:"who,attr"`
	When       string `xml:"when,attr"`
	Superseded string `xml:"superseded_by,attr"`
}

func (cred *OSCCredentials) ListRequests(ctx context.Context, req *mcp.CallToolRequest, params ListRequestsCmd) (*mcp.CallToolResult, *RequestCollection, error) {
	baseURL := fmt.Sprintf("%s/request", cred.GetAPiAddr())
	queryParams := url.Values{}
	queryParams.Set("view", "collection")

	user := params.User
	if user == "" {
		user = cred.Name
	}
	if user != "" {
		queryParams.Set("user", user)
	}

	if params.Group != "" {
		queryParams.Set("group", params.Group)
	}
	if params.Project != "" {
		queryParams.Set("project", params.Project)
	}
	if params.Package != "" {
		queryParams.Set("package", params.Package)
	}
	if params.States != "" {
		queryParams.Set("states", params.States)
	} else {
		queryParams.Set("states", "new,review")
	}
	if params.ReviewStates != "" {
		queryParams.Set("reviewstates", params.ReviewStates)
	}
	if params.Types != "" {
		queryParams.Set("types", params.Types)
	}
	if params.Roles != "" {
		queryParams.Set("roles", params.Roles)
	}
	if params.WithHistory {
		queryParams.Set("withhistory", "1")
	}
	if params.WithFullHistory {
		queryParams.Set("withfullhistory", "1")
	}
	if params.Limit > 0 {
		queryParams.Set("limit", strconv.Itoa(params.Limit))
	}

	fullURL := fmt.Sprintf("%s?%s", baseURL, queryParams.Encode())
	slog.Debug("Getting requests from OBS", "url", fullURL)

	oscReq, err := cred.buildRequest(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultClient.Do(oscReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("failed to get requests: status %s, body: %s", resp.Status, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	var requests RequestCollection
	if err := xml.Unmarshal(body, &requests); err != nil {
		slog.Debug("error on decode", "err", err, "xml", string(body))
		return nil, nil, err
	}
	if requests.Requests == nil {
		requests.Requests = make([]ShortRequest, 0)
	}
	for i := range requests.Requests {
		if requests.Requests[i].Actions == nil {
			requests.Requests[i].Actions = make([]RequestAction, 0)
		}
		for j := range requests.Requests[i].Actions {
			if requests.Requests[i].Actions[j].Persons == nil {
				requests.Requests[i].Actions[j].Persons = make([]RequestPerson, 0)
			}
			if requests.Requests[i].Actions[j].Groups == nil {
				requests.Requests[i].Actions[j].Groups = make([]RequestGroup, 0)
			}
		}
	}
	return nil, &requests, nil
}
