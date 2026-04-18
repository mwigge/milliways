package project

import (
	"encoding/json"
	"testing"
)

func TestDefaultAccessRules(t *testing.T) {
	t.Parallel()

	rules := DefaultAccessRules()

	if rules.Read != "all" {
		t.Fatalf("expected read access all, got %q", rules.Read)
	}
	if rules.Write != "project" {
		t.Fatalf("expected write access project, got %q", rules.Write)
	}
}

func TestProjectContextJSONTags(t *testing.T) {
	t.Parallel()

	ctx := ProjectContext{
		RepoRoot:         "/tmp/repo",
		RepoName:         "repo",
		Branch:           "feature/test",
		Commit:           "abc1234",
		CodeGraphPath:    "/tmp/repo/.codegraph",
		CodeGraphSymbols: 42,
		AccessRules:      DefaultAccessRules(),
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal project context: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal project context: %v", err)
	}

	for _, key := range []string{"repo_root", "repo_name", "branch", "commit", "codegraph_path", "codegraph_symbols", "access_rules"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected JSON key %q in marshalled project context", key)
		}
	}

	if _, ok := got["palace_path"]; ok {
		t.Fatal("did not expect palace_path when nil")
	}
	if _, ok := got["palace_drawers"]; ok {
		t.Fatal("did not expect palace_drawers when nil")
	}

	accessRules, ok := got["access_rules"].(map[string]any)
	if !ok {
		t.Fatal("expected access_rules to marshal as an object")
	}
	if accessRules["read"] != "all" {
		t.Fatalf("expected access_rules.read all, got %#v", accessRules["read"])
	}
	if accessRules["write"] != "project" {
		t.Fatalf("expected access_rules.write project, got %#v", accessRules["write"])
	}
}
