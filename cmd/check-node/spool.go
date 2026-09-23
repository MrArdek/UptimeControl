package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/MrArdek/UptimeControl/internal/checknodes"
)

const maximumBufferedResults = 1000

type resultSpool struct {
	mutex   sync.Mutex
	path    string
	results []checknodes.Result
}

func openSpool(path string) (*resultSpool, error) {
	spool := &resultSpool{path: path, results: []checknodes.Result{}}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return spool, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read result buffer: %w", err)
	}
	if len(content) > 8*1024*1024 {
		return nil, fmt.Errorf("result buffer exceeds 8 MiB")
	}
	if err := json.Unmarshal(content, &spool.results); err != nil {
		return nil, fmt.Errorf("decode result buffer: %w", err)
	}
	if len(spool.results) > maximumBufferedResults {
		spool.results = spool.results[len(spool.results)-maximumBufferedResults:]
	}
	return spool, nil
}

func (spool *resultSpool) append(result checknodes.Result) error {
	spool.mutex.Lock()
	defer spool.mutex.Unlock()
	if len(spool.results) >= maximumBufferedResults {
		spool.results = spool.results[1:]
	}
	spool.results = append(spool.results, result)
	return spool.persistLocked()
}

func (spool *resultSpool) batch(limit int) []checknodes.Result {
	spool.mutex.Lock()
	defer spool.mutex.Unlock()
	if limit > len(spool.results) {
		limit = len(spool.results)
	}
	return append([]checknodes.Result(nil), spool.results[:limit]...)
}

func (spool *resultSpool) acknowledge(count int) error {
	spool.mutex.Lock()
	defer spool.mutex.Unlock()
	if count > len(spool.results) {
		count = len(spool.results)
	}
	spool.results = append([]checknodes.Result(nil), spool.results[count:]...)
	return spool.persistLocked()
}

func (spool *resultSpool) len() int {
	spool.mutex.Lock()
	defer spool.mutex.Unlock()
	return len(spool.results)
}

func (spool *resultSpool) persistLocked() error {
	content, err := json.Marshal(spool.results)
	if err != nil {
		return fmt.Errorf("encode result buffer: %w", err)
	}
	directory := filepath.Dir(spool.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create result buffer directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".check-node-buffer-*")
	if err != nil {
		return fmt.Errorf("create temporary result buffer: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, spool.path); err != nil {
		return fmt.Errorf("replace result buffer: %w", err)
	}
	return nil
}
