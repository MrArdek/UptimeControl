package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/checknodes"
)

func TestSpoolPersistsAndAcknowledgesResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.json")
	spool, err := openSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	result := checknodes.Result{ResultID: "554f1140-21d8-4f83-aa18-e1243b3afce1", FinishedAt: time.Now().UTC()}
	if err := spool.append(result); err != nil {
		t.Fatal(err)
	}
	reopened, err := openSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.len() != 1 || reopened.batch(1)[0].ResultID != result.ResultID {
		t.Fatalf("reopened spool = %+v", reopened.batch(1))
	}
	if err := reopened.acknowledge(1); err != nil {
		t.Fatal(err)
	}
	if reopened.len() != 0 {
		t.Fatalf("spool length = %d", reopened.len())
	}
}
