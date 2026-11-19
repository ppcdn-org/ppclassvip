package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"ppclassvip/internal/config"
)

func TestWsInboundNormalize(t *testing.T) {
	var flat wsInbound
	if err := json.Unmarshal([]byte(`{"type":"chat.send","clientMsgId":"c1","messageType":"text","body":"hi"}`), &flat); err != nil {
		t.Fatal(err)
	}
	flat = flat.normalize()
	if flat.Type != "chat.send" || flat.ClientMsgID != "c1" || flat.MessageType != "text" || flat.Body != "hi" {
		t.Fatalf("flat = %#v", flat)
	}

	var envelope wsInbound
	if err := json.Unmarshal([]byte(`{"type":"chat.send","data":{"clientMsgId":"c2","body":"yo"}}`), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope = envelope.normalize()
	if envelope.Type != "chat.send" || envelope.ClientMsgID != "c2" || envelope.Body != "yo" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestWebsocketTokenFromProtocol(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/classrooms/1/ws", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "ppclass.token.token-value")

	token, protocol := websocketToken(req)
	if token != "token-value" {
		t.Fatalf("token = %q", token)
	}
	if protocol != "ppclass.token.token-value" {
		t.Fatalf("protocol = %q", protocol)
	}
}

func TestWebsocketTokenFromAuthorization(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/classrooms/1/ws", nil)
	req.Header.Set("Authorization", "Bearer header-token")

	token, protocol := websocketToken(req)
	if token != "header-token" || protocol != "" {
		t.Fatalf("token/protocol = %q/%q", token, protocol)
	}
}

func TestCheckWebSocketOrigin(t *testing.T) {
	h := &ClassroomHandler{config: config.Config{
		HTTP: config.HTTPConfig{CORSOrigins: []string{"https://class.example.com"}},
	}}

	allowed := httptest.NewRequest("GET", "/", nil)
	allowed.Header.Set("Origin", "https://class.example.com")
	if !h.checkWebSocketOrigin(allowed) {
		t.Fatal("expected origin to be allowed")
	}

	denied := httptest.NewRequest("GET", "/", nil)
	denied.Header.Set("Origin", "https://evil.example.com")
	if h.checkWebSocketOrigin(denied) {
		t.Fatal("expected origin to be denied")
	}

	noOrigin := httptest.NewRequest("GET", "/", nil)
	if !h.checkWebSocketOrigin(noOrigin) {
		t.Fatal("expected empty origin to be allowed")
	}
}
