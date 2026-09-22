package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoadWorkbenchSetupAndRoundTrip(t *testing.T) {
	for _, setup := range []string{
		`before_apply = ["first", { parallel = ["left", "right"] }, "last"]`,
		`before_apply = [{ parallel = ["left", "right"] }]`,
		"[[workbenches.before_apply]]\nparallel = [\"left\", \"right\"]",
		`before_apply = ["", "  first  ", "last"]`,
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			body := "[[workbenches]]\nname = \"dev\"\n" + setup + "\n[[workbenches.windows]]\nname = \"dev\"\nlayout = { command = \"sh\" }\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadWith(isolatedDeps(t), path)
			if err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			if err := toml.NewEncoder(&encoded).Encode(cfg); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			again, err := LoadWith(isolatedDeps(t), path)
			if err != nil {
				t.Fatalf("reload: %v\n%s", err, encoded.String())
			}
			if !reflect.DeepEqual(cfg.Workbenches, again.Workbenches) {
				t.Fatalf("round trip changed setup: %v -> %v", cfg.Workbenches, again.Workbenches)
			}
			if strings.Contains(setup, `"first", {`) {
				want := SetupEntries{{Command: "first"}, {Parallel: []string{"left", "right"}}, {Command: "last"}}
				if !reflect.DeepEqual(cfg.Workbenches[0].BeforeApply, want) {
					t.Fatalf("setup = %#v", cfg.Workbenches[0].BeforeApply)
				}
			}
		})
	}
}

func TestLoadRefusesMalformedWorkbenchSetup(t *testing.T) {
	for _, setup := range []string{
		`true`, `[3]`, `[{ run = ["one"] }]`, `[{ parallel = "one" }]`,
		`[{ parallel = [] }]`, `[{ parallel = ["one", 3] }]`,
		`[{ parallel = [" "] }]`, `[{ parallel = [{ parallel = ["nested"] }] }]`,
		`[{ parallel = ["one"], other = true }]`,
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("[[workbenches]]\nname = \"dev\"\nbefore_apply = "+setup), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadWith(isolatedDeps(t), path)
			if err == nil || !strings.Contains(err.Error(), "before_apply") || !(strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "expected")) {
				t.Fatalf("error = %v, want expected setup shape", err)
			}
		})
	}
}

func TestSetupKeyIsOneArrayValue(t *testing.T) {
	docs, found, isTable, _ := TableKeyDocs(ScopeGlobal, "workbenches", false)
	if !found || !isTable {
		t.Fatal("missing Workbench keys")
	}
	for _, doc := range docs {
		if doc.Key == "before_apply" && doc.Type == "array" && strings.Contains(doc.Desc, "parallel") {
			return
		}
	}
	t.Fatalf("setup array not documented: %v", docs)
}
