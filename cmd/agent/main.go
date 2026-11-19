package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	agentVersion       = "0.2.0"
	updateManifestPath = "/downloads/ppclass-agent.latest.json"
	agentDownloadPath  = "/downloads/ppclass-agent.exe"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type publishCredential struct {
	URL         string `json:"url"`
	BearerToken string `json:"bearerToken"`
	StreamPath  string `json:"streamPath"`
	ExpiresAt   string `json:"expiresAt"`
}

type pairResponse struct {
	SessionID     string             `json:"sessionId"`
	AgentToken    string             `json:"agentToken"`
	ControlWsPath string             `json:"controlWsPath"`
	Mode          string             `json:"mode"`
	TeacherName   string             `json:"teacherName"`
	ScreenPublish *publishCredential `json:"screenPublish"`
}

type updateManifest struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type controlEvent struct {
	Type      string   `json:"type"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Button    string   `json:"button"`
	Action    string   `json:"action"`
	Dx        float64  `json:"dx"`
	Dy        float64  `json:"dy"`
	Code      string   `json:"code"`
	Modifiers []string `json:"modifiers"`
}

type agentApp struct {
	server  string
	version string

	autoConsent bool

	codeCh chan string

	mu            sync.Mutex
	currentCode   string
	consentedCode string
	conn          *websocket.Conn
	capture       *captureSession
	lastPairError string
}

type captureSession struct {
	server   *http.Server
	proc     *exec.Cmd
	dir      string
	url      string
	ready    chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func main() {
	server := flag.String("server", "https://ppclassvip.pp-cdn.org", "PPClassVip server base URL")
	code := flag.String("code", "", "agent pairing code")
	checkUpdate := flag.Bool("update", true, "check for updates on startup")
	showVersion := flag.Bool("version", false, "print version and exit")
	autoConsent := flag.Bool("auto-consent", false, "skip the local consent dialog (testing only)")
	flag.Parse()
	if *showVersion {
		fmt.Println(agentVersion)
		return
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	app := &agentApp{
		server:      strings.TrimRight(*server, "/"),
		version:     agentVersion,
		codeCh:      make(chan string, 4),
		autoConsent: *autoConsent,
	}
	go app.loop()
	if *checkUpdate && strings.TrimSpace(*code) == "" {
		go app.checkForUpdate()
	}
	if strings.TrimSpace(*code) != "" {
		app.codeCh <- strings.ToUpper(strings.TrimSpace(*code))
	}
	if err := runUI(app); err != nil {
		log.Fatal(err)
	}
}

func (a *agentApp) loop() {
	for code := range a.codeCh {
		a.runSession(strings.ToUpper(strings.TrimSpace(code)))
	}
}

func (a *agentApp) runSession(code string) {
	if code == "" {
		return
	}
	a.mu.Lock()
	a.currentCode = code
	a.lastPairError = ""
	a.mu.Unlock()

	consented := false
	backoff := 2 * time.Second
	for {
		a.mu.Lock()
		active := a.currentCode == code
		a.mu.Unlock()
		if !active {
			return
		}
		resp, err := a.pair(code)
		if err != nil {
			var pairErr *pairError
			if errors.As(err, &pairErr) && pairErr.Permanent {
				a.notify("配对失败", pairErr.Message)
				a.mu.Lock()
				a.currentCode = ""
				a.mu.Unlock()
				return
			}
			log.Printf("pair failed: %v", err)
			a.mu.Lock()
			a.lastPairError = err.Error()
			a.mu.Unlock()
			time.Sleep(backoff)
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		if !consented || a.consentedCode != code {
			if !a.autoConsent && !confirmConsent(resp.TeacherName, resp.Mode) {
				a.stopRemote(resp.AgentToken)
				a.notify("已拒绝", "本地已拒绝远程控制请求。")
				a.mu.Lock()
				a.consentedCode = ""
				a.mu.Unlock()
				return
			}
			a.mu.Lock()
			a.consentedCode = code
			a.mu.Unlock()
			consented = true
		}
		if err := a.startCapture(resp); err != nil {
			a.notify("屏幕采集失败", err.Error())
			a.stopCapture()
			time.Sleep(backoff)
			continue
		}
		conn, err := a.dialControl(resp)
		if err != nil {
			log.Printf("control dial failed: %v", err)
			a.stopCapture()
			time.Sleep(backoff)
			continue
		}
		a.mu.Lock()
		a.conn = conn
		a.mu.Unlock()
		log.Printf("control connected: session=%s mode=%s", resp.SessionID, resp.Mode)
		a.readControl(conn)
		a.mu.Lock()
		if a.conn == conn {
			a.conn = nil
		}
		a.mu.Unlock()
		a.stopCapture()
		// Reconnect with the same code while it is still valid. The server
		// distinguishes network errors from permanent session/code errors.
		time.Sleep(2 * time.Second)
	}
}

type pairError struct {
	Permanent bool
	Message   string
}

func (e *pairError) Error() string { return e.Message }

func (a *agentApp) pair(code string) (*pairResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"code":     code,
		"version":  a.version,
		"platform": runtime.GOOS,
	})
	response, err := httpClient.Post(a.server+"/api/v1/agent/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var payload struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(response.Body).Decode(&payload)
		permanent := response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone || response.StatusCode == http.StatusConflict
		message := payload.Message
		if message == "" {
			message = fmt.Sprintf("server returned HTTP %d", response.StatusCode)
		}
		return nil, &pairError{Permanent: permanent, Message: message}
	}
	var envelope struct {
		Data pairResponse `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	result := envelope.Data
	if result.AgentToken == "" || result.ControlWsPath == "" {
		return nil, errors.New("invalid pair response")
	}
	return &result, nil
}

func (a *agentApp) stopRemote(token string) {
	body, _ := json.Marshal(map[string]string{"token": token})
	response, err := httpClient.Post(a.server+"/api/v1/agent/stop", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("stop remote failed: %v", err)
		return
	}
	_ = response.Body.Close()
}

func (a *agentApp) dialControl(resp *pairResponse) (*websocket.Conn, error) {
	wsURL := a.wsURL(resp.ControlWsPath)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{"ppclass.control." + resp.AgentToken},
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	return conn, err
}

func (a *agentApp) readControl(conn *websocket.Conn) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var event controlEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			continue
		}
		if err := injectEvent(event); err != nil {
			log.Printf("inject %s failed: %v", event.Type, err)
		}
	}
}

func (a *agentApp) wsURL(path string) string {
	base := a.server
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + path
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + path
	default:
		return path
	}
}

func (a *agentApp) startCapture(resp *pairResponse) error {
	a.mu.Lock()
	if a.capture != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if resp.ScreenPublish == nil || resp.ScreenPublish.URL == "" {
		return errors.New("server did not return a screen publish credential")
	}
	log.Printf("capture starting: stream=%s whip=%s", resp.ScreenPublish.StreamPath, resp.ScreenPublish.URL)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	capture := &captureSession{
		ready:   make(chan struct{}, 1),
		stopped: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/capture", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, captureHTML)
	})
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp.ScreenPublish)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		select {
		case capture.ready <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		log.Printf("capture page error: %s", strings.TrimSpace(string(data)))
		w.WriteHeader(http.StatusNoContent)
	})
	capture.server = &http.Server{Handler: mux}
	go func() { _ = capture.server.Serve(listener) }()
	capture.url = "http://" + listener.Addr().String() + "/capture"

	dir, err := os.MkdirTemp("", "ppclass-agent-browser-*")
	if err != nil {
		_ = capture.server.Close()
		return err
	}
	capture.dir = dir
	executable, err := findBrowser()
	if err != nil {
		log.Printf("browser not found: %v", err)
		_ = capture.server.Close()
		_ = os.RemoveAll(dir)
		return err
	}
	args := []string{
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-sync",
		"--disable-background-networking",
		"--use-fake-ui-for-media-stream",
		"--autoplay-policy=no-user-gesture-required",
		"--auto-select-desktop-capture-source=Entire screen",
		"--user-data-dir=" + dir,
		capture.url,
	}
	cmd := exec.Command(executable, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		log.Printf("browser start failed: %v", err)
		_ = capture.server.Close()
		_ = os.RemoveAll(dir)
		return err
	}
	log.Printf("capture browser started: pid=%d exe=%s", cmd.Process.Pid, executable)
	capture.proc = cmd
	a.mu.Lock()
	a.capture = capture
	a.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		capture.stopOnce.Do(func() { close(capture.stopped) })
	}()
	select {
	case <-capture.ready:
		log.Printf("screen capture publishing: %s", resp.ScreenPublish.URL)
	case <-time.After(15 * time.Second):
		log.Printf("screen capture did not report ready within 15s")
	}
	return nil
}

func (a *agentApp) stopCapture() {
	a.mu.Lock()
	capture := a.capture
	a.capture = nil
	a.mu.Unlock()
	if capture == nil {
		return
	}
	capture.stopOnce.Do(func() { close(capture.stopped) })
	if capture.proc != nil && capture.proc.Process != nil {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(capture.proc.Process.Pid), "/T", "/F").Run()
	}
	if capture.server != nil {
		_ = capture.server.Close()
	}
	if capture.dir != "" {
		_ = os.RemoveAll(capture.dir)
	}
}

func findBrowser() (string, error) {
	candidates := []string{
		os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
		os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`,
		os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, candidate := range candidates {
		if candidate == "" || strings.HasPrefix(candidate, `\`) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("Chrome or Edge is required for screen capture")
}

func (a *agentApp) checkForUpdate() {
	time.Sleep(2 * time.Second)
	if err := a.applyUpdate(); err != nil {
		log.Printf("update check failed: %v", err)
	}
}

func (a *agentApp) applyUpdate() error {
	response, err := httpClient.Get(a.server + updateManifestPath)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("update manifest HTTP %d", response.StatusCode)
	}
	var manifest updateManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return err
	}
	if !versionNewer(manifest.Version, a.version) {
		return nil
	}
	download, err := httpClient.Get(a.server + agentDownloadPath)
	if err != nil {
		return err
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		return fmt.Errorf("agent download HTTP %d", download.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(download.Body, 64<<20))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if manifest.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(sum[:]), manifest.SHA256) {
		return errors.New("update checksum mismatch")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	temp := filepath.Join(os.TempDir(), "ppclass-agent-update.exe")
	if err := os.WriteFile(temp, data, 0o755); err != nil {
		return err
	}
	script := fmt.Sprintf(`cmd /c ping 127.0.0.1 -n 3 >nul & move /Y "%s" "%s" & start "" "%s"`, temp, exe, exe)
	cmd := exec.Command("cmd", "/c", script)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("update %s installed; restarting", manifest.Version)
	a.stopCapture()
	os.Exit(0)
	return nil
}

func versionNewer(candidate, current string) bool {
	parse := func(value string) []int {
		parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(value, "v")), ".")
		out := make([]int, 0, len(parts))
		for _, part := range parts {
			n := 0
			for _, r := range part {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}
			out = append(out, n)
		}
		return out
	}
	a, b := parse(candidate), parse(current)
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return true
		}
		if av < bv {
			return false
		}
	}
	return false
}

func (a *agentApp) disconnect() {
	a.mu.Lock()
	a.currentCode = ""
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	a.stopCapture()
}

func (a *agentApp) notify(title, message string) {
	log.Printf("%s: %s", title, message)
	if !a.autoConsent {
		showMessage(title, message)
	}
}

const captureHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>PPClass capture</title>
<style>html,body{background:#000;margin:0;height:100%;color:#0f0;font:14px monospace}</style>
</head><body><pre id="log">starting screen capture…</pre>
<script>
const logEl = document.getElementById('log');
function log(text) { logEl.textContent += '\n' + text; }
async function reportError(message) {
  log('ERROR ' + message);
  try { await fetch('/error', { method: 'POST', body: String(message) }); } catch {}
}
async function waitIce(pc, timeoutMs) {
  if (pc.iceGatheringState === 'complete') return;
  await new Promise((resolve) => {
    const done = () => { pc.removeEventListener('icegatheringstatechange', onChange); clearTimeout(timer); resolve(); };
    const onChange = () => { if (pc.iceGatheringState === 'complete') done(); };
    const timer = setTimeout(done, timeoutMs);
    pc.addEventListener('icegatheringstatechange', onChange);
  });
}
async function start() {
  try {
    const config = await fetch('/config').then((r) => r.json());
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video: { width: { ideal: 1920 }, height: { ideal: 1080 }, frameRate: { ideal: 15 } },
      audio: false,
    });
    const track = stream.getVideoTracks()[0];
    if (!track) throw new Error('no screen video track');
    track.contentHint = 'text';
    const pc = new RTCPeerConnection({ iceServers: [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] }] });
    const transceiver = pc.addTransceiver(track, { direction: 'sendonly', streams: [stream] });
    const caps = RTCRtpSender.getCapabilities('video');
    const h264 = caps && caps.codecs ? caps.codecs.find((c) => /h264/i.test(c.mimeType)) : null;
    if (h264 && transceiver.setCodecPreferences) transceiver.setCodecPreferences([h264, ...caps.codecs.filter((c) => c !== h264)]);
    await pc.setLocalDescription(await pc.createOffer());
    await waitIce(pc, 3000);
    const response = await fetch(config.url, {
      method: 'POST',
      headers: { Authorization: 'Bearer ' + config.bearerToken, 'Content-Type': 'application/sdp' },
      body: pc.localDescription.sdp,
    });
    if (!response.ok) throw new Error('WHIP publish failed: HTTP ' + response.status + ' ' + (await response.text()).slice(0, 200));
    await pc.setRemoteDescription({ type: 'answer', sdp: await response.text() });
    await fetch('/ready', { method: 'POST' });
    log('publishing ' + config.streamPath);
  } catch (error) {
    await reportError(error && error.message ? error.message : error);
    setTimeout(start, 3000);
  }
}
start();
</script></body></html>`

func (a *agentApp) promptCode() (string, error) {
	return promptPairingCode()
}

func runConsoleUI(a *agentApp) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("PPClass remote assist agent %s (%s)\n", agentVersion, runtime.GOOS)
	for {
		fmt.Print("Enter pairing code: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		code := strings.ToUpper(strings.TrimSpace(line))
		if code != "" {
			a.codeCh <- code
		}
		if err == io.EOF {
			return nil
		}
	}
}
