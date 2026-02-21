package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestShouldSkipFinderDir(t *testing.T) {
	cases := []struct {
		name string
		skip bool
	}{
		{name: ".git", skip: true},
		{name: "node_modules", skip: true},
		{name: "vendor", skip: true},
		{name: "src", skip: false},
	}

	for _, tc := range cases {
		got := shouldSkipFinderDir(tc.name)
		if got != tc.skip {
			t.Errorf("shouldSkipFinderDir(%q) = %v, want %v", tc.name, got, tc.skip)
		}
	}
}

func TestCollectFinderFiles(t *testing.T) {
	tmp := t.TempDir()

	a := filepath.Join(tmp, "a.txt")
	b := filepath.Join(tmp, "nested", "b.txt")
	c := filepath.Join(tmp, "node_modules", "skip.txt")
	d := filepath.Join(tmp, "vendor", "skip2.txt")
	e := filepath.Join(tmp, ".git", "skip3.txt")

	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(c), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(c, []byte("skip"), 0o644); err != nil {
		t.Fatalf("write c: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	if err := os.WriteFile(d, []byte("skip2"), 0o644); err != nil {
		t.Fatalf("write d: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(e), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(e, []byte("skip3"), 0o644); err != nil {
		t.Fatalf("write e: %v", err)
	}

	files, err := collectFinderFiles(tmp)
	if err != nil {
		t.Fatalf("collectFinderFiles: %v", err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Rel)
	}

	want := []string{"a.txt", filepath.ToSlash(filepath.Join("nested", "b.txt"))}

	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d: %v", len(got), len(want), got)
	}

	sort.Strings(got)
	for i, w := range want {
		if got[i] != w {
			t.Errorf("files[%d] = %q, want %q", i, got[i], w)
		}
	}

	dirs := make([]string, len(files))
	for i, f := range files {
		dirs[i] = f.Abs
	}
	for _, p := range dirs {
		if strings.Contains(p, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
			t.Errorf("collectFinderFiles should skip node_modules: %q", p)
		}
		if strings.Contains(p, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
			t.Errorf("collectFinderFiles should skip vendor: %q", p)
		}
		if strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			t.Errorf("collectFinderFiles should skip .git: %q", p)
		}
	}
}
