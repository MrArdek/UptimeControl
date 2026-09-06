package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type heartbeatRecorder struct {
	token string
}

func (recorder *heartbeatRecorder) RecordHeartbeat(_ context.Context, token string) error {
	recorder.token = token
	return nil
}

func TestHeartbeatHandlerRecordsValidToken(t *testing.T) {
	recorder := &heartbeatRecorder{}
	token := "1234567890123456789012345678901234567890123"
	request := httptest.NewRequest(http.MethodPost, "/uptimec/api/v1/heartbeat/"+token, nil)
	response := httptest.NewRecorder()
	heartbeatHandler(recorder, "/uptimec")(response, request)

	if response.Code != http.StatusNoContent || recorder.token != token {
		t.Fatalf("heartbeat status=%d token=%q", response.Code, recorder.token)
	}
}

func TestHeartbeatHandlerHidesInvalidToken(t *testing.T) {
	recorder := &heartbeatRecorder{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat/short", nil)
	response := httptest.NewRecorder()
	heartbeatHandler(recorder, "")(response, request)

	if response.Code != http.StatusNoContent || recorder.token != "" {
		t.Fatalf("invalid heartbeat status=%d token=%q", response.Code, recorder.token)
	}
}
