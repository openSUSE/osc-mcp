package osc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchBundle(t *testing.T) {
	testCases := []struct {
		name         string
		status       int
		body         string
		wantProject  string
		wantExisting bool
		wantText     string
		wantErr      string
	}{
		{
			name:   "branch exists, check out where OBS reports it",
			status: http.StatusBadRequest,
			// body api.opensuse.org returned for a package whose branch exists already
			body: `<status code="double_branch_package">
  <summary>branch target package already exists: home:hsharma:branches:devel:languages:python/python-nox</summary>
</status>
`,
			wantProject:  "home:hsharma:branches:devel:languages:python",
			wantExisting: true,
			wantText:     "existing branch checked out at home:hsharma:branches:devel:languages:python/python-nox (it may be behind devel:languages:python/python-nox;",
		},
		{
			name:         "branch exists, summary names no location",
			status:       http.StatusBadRequest,
			body:         `<status code="double_branch_package"/>`,
			wantProject:  "home:testuser:branches:devel:languages:python",
			wantExisting: true,
			wantText:     "existing branch checked out at home:testuser:branches:devel:languages:python/python-nox",
		},
		{
			name:    "branch exists, reported location is not a plain name",
			status:  http.StatusBadRequest,
			body:    `<status code="double_branch_package"><summary>branch target package already exists: ../python-nox</summary></status>`,
			wantErr: "unexpected branch location in OBS answer",
		},
		{
			name:    "other code with an existing-branch summary",
			status:  http.StatusBadRequest,
			body:    `<status code="invalid_project_name"><summary>branch target package already exists: a/b</summary></status>`,
			wantErr: "invalid_project_name",
		},
		{
			name:    "other bad request",
			status:  http.StatusBadRequest,
			body:    `<status code="invalid_project_name"><summary>invalid project name 'foo bar'</summary></status>`,
			wantErr: "invalid project name 'foo bar'",
		},
		{
			name:        "branch created",
			status:      http.StatusOK,
			body:        `<status code="ok"><summary>Ok</summary></status>`,
			wantProject: "home:testuser:branches:devel:languages:python",
			wantText:    "branch created at home:testuser:branches:devel:languages:python/python-nox",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// the checkout runs osc, put one on PATH that does nothing
			binDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "osc"), []byte("#!/bin/sh\n"), 0o755))
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/source/devel:languages:python/python-nox", r.URL.Path)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			cred := OSCCredentials{
				Name:    "testuser",
				Passwd:  "testpassword",
				Apiaddr: server.URL,
				TempDir: t.TempDir(),
			}
			res, result, err := cred.BranchBundle(context.Background(), &mcp.CallToolRequest{Session: &mcp.ServerSession{}}, BranchPackageParam{
				Project: "devel:languages:python",
				Bundle:  "python-nox",
			})

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantProject, result.TargetProject)
			assert.Equal(t, "python-nox", result.TargetPackage)
			assert.Equal(t, filepath.Join(cred.TempDir, tc.wantProject, "python-nox"), result.CheckoutDir)
			assert.Equal(t, tc.wantExisting, result.Existing)
			require.Len(t, res.Content, 2)
			assert.Contains(t, res.Content[1].(*mcp.TextContent).Text, tc.wantText)
			assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, `"checkout_dir"`)
		})
	}
}
