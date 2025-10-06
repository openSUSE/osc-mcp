package osc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestListRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualURL, err := url.Parse(r.URL.String())
		assert.NoError(t, err)
		assert.Equal(t, "testuser", actualURL.Query().Get("user"))
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

	_, requests, err := cred.ListRequests(context.Background(), &mcp.CallToolRequest{}, ListRequestsCmd{User: "testuser"})
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

func TestGetRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualURL, err := url.Parse(r.URL.String())
		assert.NoError(t, err)
		if actualURL.Query().Has("cmd") {
			expectedURL, err := url.Parse("/request/123?cmd=diff")
			assert.NoError(t, err)
			assert.Equal(t, expectedURL.Query(), actualURL.Query())
		} else {
			expectedURL, err := url.Parse("/request/123?withhistory=1")
			assert.NoError(t, err)
			assert.Equal(t, expectedURL.Query(), actualURL.Query())
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `
<request id="123" creator="testuser" created="2025-09-22T10:00:00">
  <action type="submit">
    <source project="home:testuser" package="testpackage" rev="1"/>
    <target project="openSUSE:Factory" package="testpackage"/>
    <person name="testreviewer" role="reviewer"/>
    <group name="opensuse-review-team" role="reviewer"/>
  </action>
  <state name="review" who="testreviewer" when="2025-09-22T11:00:00"/>
  <description>Please review my package.</description>
  <history who="testuser" when="2025-09-22T10:00:00">submitted</history>
  <review state="new" who="testreviewer" when="2025-09-22T11:00:00" by_user="testreviewer"/>
</request>
`)
	}))
	defer server.Close()

	cred := &OSCCredentials{
		Name:    "testuser",
		Passwd:  "testpassword",
		Apiaddr: server.URL,
	}

	_, request, err := cred.GetRequest(context.Background(), &mcp.CallToolRequest{}, GetRequestCmd{Id: "123"})
	assert.NoError(t, err)
	assert.NotNil(t, request)
	assert.Equal(t, "123", request.ID)
	assert.Equal(t, "testuser", request.Creator)
	assert.Len(t, request.Actions, 1)
	assert.Equal(t, "submit", request.Actions[0].Type)
	assert.Equal(t, "home:testuser", request.Actions[0].Source.Project)
	assert.Equal(t, "testpackage", request.Actions[0].Source.Package)
	assert.Equal(t, "1", request.Actions[0].Source.Rev)
	assert.Equal(t, "openSUSE:Factory", request.Actions[0].Target.Project)
	assert.Equal(t, "testpackage", request.Actions[0].Target.Package)
	assert.Len(t, request.Actions[0].Persons, 1)
	assert.Equal(t, "testreviewer", request.Actions[0].Persons[0].Name)
	assert.Equal(t, "reviewer", request.Actions[0].Persons[0].Role)
	assert.Len(t, request.Actions[0].Groups, 1)
	assert.Equal(t, "opensuse-review-team", request.Actions[0].Groups[0].Name)
	assert.Equal(t, "reviewer", request.Actions[0].Groups[0].Role)
	assert.Equal(t, "review", request.State.Name)
	assert.Equal(t, "testreviewer", request.State.Who)
	assert.Equal(t, "2025-09-22T11:00:00", request.State.When)
	assert.Equal(t, "Please review my package.", request.Description)
	assert.Len(t, request.Histories, 1)
	assert.Equal(t, "testuser", request.Histories[0].Who)
	assert.Equal(t, "2025-09-22T10:00:00", request.Histories[0].When)
	assert.Equal(t, "submitted", request.Histories[0].Comment)
	assert.Len(t, request.Reviews, 1)
	assert.Equal(t, "new", request.Reviews[0].State)
	assert.Equal(t, "testreviewer", request.Reviews[0].Who)
	assert.Equal(t, "2025-09-22T11:00:00", request.Reviews[0].When)
	assert.Equal(t, "testreviewer", request.Reviews[0].ByUser)
}



func TestGetRequest_Error(t *testing.T) {
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

	_, _, err := cred.GetRequest(context.Background(), &mcp.CallToolRequest{}, GetRequestCmd{Id: "123"})
	assert.Error(t, err)
}
