package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MrArdek/UptimeControl/internal/checknodes"
)

type backendClient struct {
	baseURL string
	secret  string
	client  *http.Client
}

func newBackendClient(config nodeConfig) *backendClient {
	return &backendClient{baseURL: config.BackendURL, secret: config.Secret, client: &http.Client{Timeout: 30 * time.Second}}
}

func (client *backendClient) assignments(ctx context.Context) ([]checknodes.Assignment, error) {
	var response struct {
		Assignments []checknodes.Assignment `json:"assignments"`
	}
	if err := client.request(ctx, http.MethodGet, "/api/v1/monitoring/assignments?limit=25", nil, &response); err != nil {
		return nil, err
	}
	return response.Assignments, nil
}

func (client *backendClient) results(ctx context.Context, results []checknodes.Result) (checknodes.BatchOutcome, error) {
	var outcome checknodes.BatchOutcome
	err := client.request(ctx, http.MethodPost, "/api/v1/ingest/monitoring/results", struct {
		Results []checknodes.Result `json:"results"`
	}{Results: results}, &outcome)
	return outcome, err
}

func (client *backendClient) request(ctx context.Context, method, path string, payload, destination any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.secret)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("backend request: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 1024*1024)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(limited).Decode(&failure)
		if failure.Error.Code == "" {
			failure.Error.Code = response.Status
		}
		return fmt.Errorf("backend rejected request: %s", failure.Error.Code)
	}
	if destination != nil {
		if err := json.NewDecoder(limited).Decode(destination); err != nil {
			return fmt.Errorf("decode backend response: %w", err)
		}
	}
	return nil
}
