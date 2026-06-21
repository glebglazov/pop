package integration

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

func integrationDataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	return d
}

func TestAwaitingIntegrationFiltersNoopRecords(t *testing.T) {
	td := integrationDataDeps(t)
	key := binding.ScopedKey("repo", "noop")
	if err := Save(td, &Store{Records: map[string]Record{
		key: {
			SetID:  "noop",
			Status: StatusClean,
			Target: "same",
			Source: "same",
		},
	}}); err != nil {
		t.Fatal(err)
	}

	items, err := AwaitingIntegration(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("awaiting integration = %+v, want empty", items)
	}
	if _, ok, err := GetForSet(td, "repo", "noop"); err != nil || ok {
		t.Fatalf("GetForSet ok=%v err=%v, want missing", ok, err)
	}
}

func TestRecordMergeabilityDeletesNoopRecord(t *testing.T) {
	td := integrationDataDeps(t)
	repo := initMergeabilityRepo(t)
	key, err := ScopedKeyForPaths(td, repo, repo, "noop")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(td, &Store{Records: map[string]Record{
		key: {
			SetID:  "noop",
			Status: StatusClean,
			Target: "old-target",
			Source: "old-source",
		},
	}}); err != nil {
		t.Fatal(err)
	}

	err = RecordMergeability(&Deps{Tasks: td}, repo, Record{
		RuntimePath: repo,
		SetID:       "noop",
		Status:      StatusClean,
		Target:      "same",
		Source:      "same",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Records) != 0 {
		t.Fatalf("records = %+v, want empty", store.Records)
	}
}
