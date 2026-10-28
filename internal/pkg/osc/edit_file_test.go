package osc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInWorkdir(t *testing.T) {
	cred := &OSCCredentials{TempDir: "/tmp/osc-mcp"}
	testCases := []struct {
		dir string
		ok  bool
	}{
		{"/tmp/osc-mcp", true},
		{"/tmp/osc-mcp/home:user:branches:devel/pkg", true},
		{"/tmp/osc-mcp/prj/../pkg", true},
		{"/tmp/osc-mcp-other/pkg", false},
		{"/tmp/osc-mcp/../etc", false},
		{"/etc", false},
	}
	for _, tc := range testCases {
		t.Run(tc.dir, func(t *testing.T) {
			_, err := cred.inWorkdir(tc.dir)
			if (err == nil) != tc.ok {
				t.Errorf("inWorkdir(%q) error = %v, want ok = %v", tc.dir, err, tc.ok)
			}
		})
	}
}

func TestEditFile(t *testing.T) {
	workdir := t.TempDir()
	cred := &OSCCredentials{TempDir: workdir}
	req := &mcp.CallToolRequest{Session: &mcp.ServerSession{}}
	pkgDir := filepath.Join(workdir, "prj", "pkg")
	// a sibling sharing the workdir name as prefix must not count as inside it
	for _, dir := range []string{pkgDir, workdir + "-other"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := cred.EditFile(context.Background(), req, EditFileParams{Directory: pkgDir, Filename: "pkg.spec", Content: "Name: pkg\n"}); err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(pkgDir, "pkg.spec")); string(got) != "Name: pkg\n" {
		t.Errorf("unexpected file content %q", got)
	}

	// symlinks inside the workdir that point out of it must not be written through
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workdir, "prj", "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(pkgDir, "link.spec")); err != nil {
		t.Fatal(err)
	}

	for _, params := range []EditFileParams{
		{Directory: pkgDir, Filename: "../escape", Content: "x"},
		{Directory: pkgDir, Filename: "..", Content: "x"},
		{Directory: workdir + "-other", Filename: "escape", Content: "x"},
		{Directory: filepath.Join(workdir, "prj", "linkdir"), Filename: "new", Content: "x"},
		{Directory: pkgDir, Filename: "link.spec", Content: "x"},
	} {
		if _, _, err := cred.EditFile(context.Background(), req, params); err == nil {
			t.Errorf("EditFile(%+v) succeeded, want error", params)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "target")); string(got) != "keep" {
		t.Errorf("file outside the workdir changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Errorf("file created outside the workdir")
	}
}
