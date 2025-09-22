package osc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestGetRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/request?states=new%2Creview&user=testuser&view=collection", r.URL.String())
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `
<collection matches="1">
  <request id="123" creator="testuser" created="2025-09-22T10:00:00">
    <action type="submit">
      <source project="home:testuser" package="testpackage" rev="1"/>
      <target project="openSUSE:Factory" package="testpackage"/>
      <person name="testreviewer" role="reviewer"/>
      <group name="opensuse-review-team" role="reviewer"/>
    </action>
    <state name="review" who="testreviewer" when="2025-09-22T11:00:00"/>
    <description>Please review my package.</description>
  </request>
</collection>
`)
	}))
	defer server.Close()

	cred := &OSCCredentials{
		Name:    "testuser",
		Passwd:  "testpassword",
		Apiaddr: server.URL,
	}

	_, requests, err := cred.GetRequests(context.Background(), &mcp.CallToolRequest{}, GetRequestsCmd{User: "testuser"})
	assert.NoError(t, err)
	assert.NotNil(t, requests)
	assert.Equal(t, "1", requests.Matches)
	assert.Len(t, requests.Requests, 1)
	assert.Equal(t, "123", requests.Requests[0].ID)
	assert.Equal(t, "testuser", requests.Requests[0].Creator)
	assert.Len(t, requests.Requests[0].Actions, 1)
	assert.Equal(t, "submit", requests.Requests[0].Actions[0].Type)
	assert.Equal(t, "home:testuser", requests.Requests[0].Actions[0].Source.Project)
	assert.Equal(t, "testpackage", requests.Requests[0].Actions[0].Source.Package)
	assert.Equal(t, "1", requests.Requests[0].Actions[0].Source.Rev)
	assert.Equal(t, "openSUSE:Factory", requests.Requests[0].Actions[0].Target.Project)
	assert.Equal(t, "testpackage", requests.Requests[0].Actions[0].Target.Package)
	assert.Len(t, requests.Requests[0].Actions[0].Persons, 1)
	assert.Equal(t, "testreviewer", requests.Requests[0].Actions[0].Persons[0].Name)
	assert.Equal(t, "reviewer", requests.Requests[0].Actions[0].Persons[0].Role)
	assert.Len(t, requests.Requests[0].Actions[0].Groups, 1)
	assert.Equal(t, "opensuse-review-team", requests.Requests[0].Actions[0].Groups[0].Name)
	assert.Equal(t, "reviewer", requests.Requests[0].Actions[0].Groups[0].Role)
	assert.Equal(t, "review", requests.Requests[0].State.Name)
	assert.Equal(t, "testreviewer", requests.Requests[0].State.Who)
	assert.Equal(t, "2025-09-22T11:00:00", requests.Requests[0].State.When)
	assert.Equal(t, "Please review my package.", requests.Requests[0].Description)
}

func TestGetRequests_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<collection matches="0"></collection>`)
	}))
	defer server.Close()

	cred := &OSCCredentials{
		Name:    "testuser",
		Passwd:  "testpassword",
		Apiaddr: server.URL,
	}

	_, requests, err := cred.GetRequests(context.Background(), &mcp.CallToolRequest{}, GetRequestsCmd{User: "testuser"})
	assert.NoError(t, err)
	assert.NotNil(t, requests)
	assert.Equal(t, "0", requests.Matches)
	assert.Len(t, requests.Requests, 0)
}

func TestGetRequests_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal Server Error")
	}))
	defer server.Close()

	cred := &OSCCredentials{
		Name:    "testuser",
		Passwd:  "testpassword",
		Apiaddr: server.URL,
	}

	_, _, err := cred.GetRequests(context.Background(), &mcp.CallToolRequest{}, GetRequestsCmd{User: "testuser"})
	assert.Error(t, err)
}
