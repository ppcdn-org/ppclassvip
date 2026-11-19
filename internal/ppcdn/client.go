package ppcdn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ppclassvip/internal/config"
)

type Client struct {
	baseURL   string
	appID     string
	appSecret string
	keyID     string
	deviceID  string
	region    string
	ttl       time.Duration
	http      *http.Client
}

func NewClient(cfg config.PPCDNConfig) *Client {
	keyID := strings.TrimSpace(cfg.KeyID)
	if keyID == "" {
		keyID = "default"
	}
	return &Client{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		appID:     strings.TrimSpace(cfg.AppID),
		appSecret: strings.TrimSpace(cfg.AppSecret),
		keyID:     keyID,
		deviceID:  strings.TrimSpace(cfg.DeviceID),
		region:    strings.TrimSpace(cfg.Region),
		ttl:       cfg.MediaTTL.Std(),
		http:      &http.Client{Timeout: cfg.Timeout.Std()},
	}
}

func (c *Client) Publish(ctx context.Context, streamName string) (Credential, error) {
	payload := map[string]any{
		"appId":        c.appID,
		"appSecret":    c.appSecret,
		"streamName":   streamName,
		"capabilities": []string{"whip-hevc-h264"},
	}
	if c.region != "" {
		payload["requestRegion"] = c.region
	}
	if c.deviceID != "" {
		payload["deviceId"] = c.deviceID
	}
	var decision struct {
		WHIPURL     string    `json:"whipUrl"`
		BearerToken string    `json:"bearerToken"`
		ExpiresAt   time.Time `json:"expiresAt"`
		WHIPTracks  map[string]struct {
			URL         string `json:"url"`
			BearerToken string `json:"bearerToken"`
		} `json:"whipTracks"`
	}
	if err := c.postJSON(ctx, "/v1/publish/requests", "", payload, &decision); err != nil {
		return Credential{}, err
	}
	// The bare (codec-less) WHIP path is the legacy single-track endpoint. A
	// codec-suffixed track is what the edge exposes as "/h264/whep", which is
	// the path our WHEP player requests, so prefer it when ppcenter provides it.
	if track, ok := decision.WHIPTracks["h264"]; ok && strings.TrimSpace(track.URL) != "" {
		return Credential{
			URL:         track.URL,
			BearerToken: track.BearerToken,
			Codec:       "h264",
			StreamPath:  c.appID + "/" + streamName + "/h264",
			ExpiresAt:   formatTime(decision.ExpiresAt),
		}, nil
	}
	if strings.TrimSpace(decision.WHIPURL) == "" {
		return Credential{}, &Error{Status: http.StatusServiceUnavailable, Code: "invalid_publish_decision", Message: "ppcenter returned no WHIP URL"}
	}
	return Credential{
		URL:         decision.WHIPURL,
		BearerToken: decision.BearerToken,
		StreamPath:  c.appID + "/" + streamName,
		ExpiresAt:   formatTime(decision.ExpiresAt),
	}, nil
}

func (c *Client) Subscribe(ctx context.Context, streamName string) (Credential, error) {
	streamPath := c.appID + "/" + streamName
	expiresAt := time.Now().Add(c.ttl)
	txTime := strconv.FormatInt(expiresAt.Unix(), 16)
	txSecret := SignV1(c.keyID, c.appSecret, streamPath, txTime)
	payload := map[string]any{
		"streamName":   streamName,
		"clientId":     "ppclassvip",
		"capabilities": []string{"whep"},
		"codec":        "h264",
	}
	if c.region != "" {
		payload["requestRegion"] = c.region
	}
	authorization := "Bearer " + c.appID + ":" + txTime + ":" + txSecret
	var decision struct {
		EdgeStreamURL string    `json:"edgeStreamUrl"`
		ExpiresAt     time.Time `json:"expiresAt"`
	}
	if err := c.postJSON(ctx, "/v1/play/requests", authorization, payload, &decision); err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(decision.EdgeStreamURL) == "" {
		return Credential{}, &Error{Status: http.StatusServiceUnavailable, Code: "invalid_play_decision", Message: "ppcenter returned no edge URL"}
	}
	return Credential{
		URL:        decision.EdgeStreamURL,
		Codec:      "h264",
		StreamPath: streamPath,
		ExpiresAt:  formatTime(decision.ExpiresAt),
	}, nil
}

func (c *Client) postJSON(ctx context.Context, path, authorization string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	requestURL := c.baseURL + path
	if _, err := url.ParseRequestURI(requestURL); err != nil {
		return &Error{Status: http.StatusServiceUnavailable, Code: "invalid_ppcdn_url", Message: "invalid ppcdn base URL"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Status: http.StatusServiceUnavailable, Code: "ppcdn_unavailable", Message: err.Error()}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &apiErr)
		return &Error{Status: resp.StatusCode, Code: apiErr.Code, Message: apiErr.Message}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode ppcenter response: %w", err)
	}
	return nil
}

func SignV1(keyID, secret, streamPath, txTime string) string {
	keyID = strings.ToLower(strings.TrimSpace(keyID))
	if keyID == "" {
		keyID = "default"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(keyID))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.Trim(streamPath, "/")))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.ToUpper(txTime)))
	return "v1." + keyID + "." + hex.EncodeToString(mac.Sum(nil))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
