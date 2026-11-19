package ppcdn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ppclassvip/internal/config"
)

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return NewClient(config.PPCDNConfig{
		Enabled:   true,
		BaseURL:   serverURL,
		AppID:     "app123",
		AppSecret: "secret-value",
		KeyID:     "default",
		Timeout:   config.Duration(2 * time.Second),
		MediaTTL:  config.Duration(5 * time.Minute),
	})
}

func TestPublishCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/publish/requests" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["appId"] != "app123" || payload["appSecret"] != "secret-value" || payload["streamName"] != "cls7-t" {
			t.Fatalf("payload = %#v", payload)
		}
		capabilities, _ := payload["capabilities"].([]any)
		if len(capabilities) != 1 || capabilities[0] != "whip-hevc-h264" {
			t.Fatalf("capabilities = %#v", payload["capabilities"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"whipUrl":     "https://origin.example/whip/cls7-t",
			"bearerToken": "token-1",
			"expiresAt":   time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	credential, err := testClient(t, server.URL).Publish(context.Background(), "cls7-t")
	if err != nil {
		t.Fatal(err)
	}
	if credential.URL != "https://origin.example/whip/cls7-t" || credential.BearerToken != "token-1" {
		t.Fatalf("credential = %#v", credential)
	}
	if credential.StreamPath != "app123/cls7-t" {
		t.Fatalf("stream path = %q", credential.StreamPath)
	}
}

func TestPublishPrefersH264Track(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"whipUrl":     "https://origin.example/whip/cls7-t",
			"bearerToken": "bare-token",
			"expiresAt":   time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
			"whipTracks": map[string]any{
				"h264": map[string]any{
					"url":         "https://origin.example/whip/cls7-t/h264",
					"bearerToken": "h264-token",
				},
				"hevc": map[string]any{
					"url":         "https://origin.example/whip/cls7-t/hevc",
					"bearerToken": "hevc-token",
				},
			},
		})
	}))
	defer server.Close()

	credential, err := testClient(t, server.URL).Publish(context.Background(), "cls7-t")
	if err != nil {
		t.Fatal(err)
	}
	if credential.URL != "https://origin.example/whip/cls7-t/h264" || credential.BearerToken != "h264-token" {
		t.Fatalf("credential = %#v", credential)
	}
	if credential.Codec != "h264" || credential.StreamPath != "app123/cls7-t/h264" {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestSubscribeCredentialUsesViewerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/play/requests" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		header := r.Header.Get("Authorization")
		parts := strings.Split(strings.TrimPrefix(header, "Bearer "), ":")
		if len(parts) != 3 || parts[0] != "app123" || parts[1] == "" {
			t.Fatalf("authorization = %q", header)
		}
		expected := SignV1("default", "secret-value", "app123/cls7-s", parts[1])
		if parts[2] != expected {
			t.Fatalf("txSecret = %q want %q", parts[2], expected)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode":          "edge-only",
			"edgeStreamUrl": "https://edge.example/app123/cls7-s?txTime=1&txSecret=2",
			"expiresAt":     time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	credential, err := testClient(t, server.URL).Subscribe(context.Background(), "cls7-s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(credential.URL, "edge.example") || credential.Codec != "h264" {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestPublishErrorFromPPCDN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "no_healthy_origin", "message": "no origin"})
	}))
	defer server.Close()

	_, err := testClient(t, server.URL).Publish(context.Background(), "cls7-t")
	mediaErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if mediaErr.Code != "no_healthy_origin" || mediaErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("media error = %#v", mediaErr)
	}
}

func TestStreamNames(t *testing.T) {
	cases := map[string]string{
		LegTeacherCamera: "cls42-t",
		LegStudentCamera: "cls42-s",
		LegTeacherScreen: "cls42-ts",
		LegStudentScreen: "cls42-ss",
	}
	for leg, expected := range cases {
		if got := StreamName(42, leg); got != expected {
			t.Fatalf("StreamName(%s) = %q want %q", leg, got, expected)
		}
	}
	if CameraLeg("student") != LegStudentCamera || PeerCameraLeg("student") != LegTeacherCamera {
		t.Fatal("student leg mapping is wrong")
	}
}
