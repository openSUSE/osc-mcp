package osc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReplaceFile(t *testing.T) {
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

	for _, dir := range []string{pkgDir, filepath.Join(workdir, "prj", "..", "prj", "pkg"), workdir} {
		if _, _, err := cred.ReplaceFile(context.Background(), req, ReplaceFileParams{Directory: dir, Filename: "pkg.spec", Content: "Name: pkg\n"}); err != nil {
			t.Fatalf("ReplaceFile in %s failed: %v", dir, err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "pkg.spec")); string(got) != "Name: pkg\n" {
			t.Errorf("unexpected file content %q", got)
		}
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
	if err := os.Symlink("pkg.spec", filepath.Join(pkgDir, "inside.link")); err != nil {
		t.Fatal(err)
	}

	for _, params := range []ReplaceFileParams{
		{Directory: pkgDir, Filename: "../escape", Content: "x"},
		{Directory: pkgDir, Filename: "..", Content: "x"},
		{Directory: workdir + "-other", Filename: "escape", Content: "x"},
		{Directory: filepath.Join(workdir, "..", filepath.Base(outside)), Filename: "new", Content: "x"},
		{Directory: outside, Filename: "new", Content: "x"},
		{Directory: filepath.Join(workdir, "prj", "linkdir"), Filename: "new", Content: "x"},
		{Directory: pkgDir, Filename: "link.spec", Content: "x"},
		{Directory: pkgDir, Filename: "inside.link", Content: "x"},
		{Directory: filepath.Join(workdir, "missing"), Filename: "new", Content: "x"},
	} {
		if _, _, err := cred.ReplaceFile(context.Background(), req, params); err == nil {
			t.Errorf("ReplaceFile(%+v) succeeded, want error", params)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "target")); string(got) != "keep" {
		t.Errorf("file outside the workdir changed: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(pkgDir, "pkg.spec")); string(got) != "Name: pkg\n" {
		t.Errorf("file behind a symlink changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Errorf("file created outside the workdir")
	}
}
