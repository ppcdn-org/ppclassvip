package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultServer      = "https://ppclassvip.pp-cdn.org"
	updateManifestPath = "/downloads/ppclass-agent.latest.json"
	agentDownloadPath  = "/downloads/ppclass-agent.exe"

	runValueName = "PPClassAgent"
)

type updateManifest struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func main() {
	server := flag.String("server", defaultServer, "PPClassVip server base URL")
	uninstall := flag.Bool("uninstall", false, "uninstall PPClass Agent")
	quiet := flag.Bool("quiet", false, "suppress dialogs")
	flag.Parse()

	var err error
	if *uninstall {
		err = uninstallAgent(*quiet)
	} else {
		err = installAgent(strings.TrimRight(*server, "/"), *quiet)
	}
	if err != nil {
		if *quiet {
			fmt.Fprintln(os.Stderr, err)
		} else {
			notify("PPClass Agent", "操作失败：\n"+err.Error())
		}
		os.Exit(1)
	}
}

func installAgent(server string, quiet bool) error {
	client := &http.Client{Timeout: 90 * time.Second}
	manifest, err := fetchManifest(client, server)
	if err != nil {
		return err
	}
	data, err := download(client, server+agentDownloadPath)
	if err != nil {
		return err
	}
	if manifest.SHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), manifest.SHA256) {
			return errors.New("安装包校验失败")
		}
	}

	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return errors.New("无法定位 LOCALAPPDATA")
	}
	targetDir := filepath.Join(base, "PPClass", "Agent")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(targetDir, "ppclass-agent.exe")
	_ = exec.Command("taskkill", "/IM", "ppclass-agent.exe", "/F").Run()
	time.Sleep(700 * time.Millisecond)
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		writeErr = os.WriteFile(target, data, 0o755)
		if writeErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if writeErr != nil {
		return writeErr
	}
	_ = os.WriteFile(filepath.Join(targetDir, "version.txt"), []byte(manifest.Version+"\n"), 0o644)

	if err := setAutostart(target); err != nil {
		return fmt.Errorf("设置开机启动失败: %w", err)
	}
	_ = exec.Command("cmd", "/c", "start", "", target).Start()
	if !quiet {
		notify("PPClass Agent", fmt.Sprintf("安装完成（%s）。\n\nAgent 已设置为登录 Windows 后自动启动，并已放入系统托盘。", manifest.Version))
	}
	return nil
}

func uninstallAgent(quiet bool) error {
	_ = exec.Command("taskkill", "/IM", "ppclass-agent.exe", "/F").Run()
	_ = removeAutostart()
	base := os.Getenv("LOCALAPPDATA")
	if base != "" {
		_ = os.RemoveAll(filepath.Join(base, "PPClass", "Agent"))
	}
	if !quiet {
		notify("PPClass Agent", "已卸载，并移除了开机启动项。")
	}
	return nil
}

func fetchManifest(client *http.Client, server string) (*updateManifest, error) {
	response, err := client.Get(server + updateManifestPath)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取版本信息失败: HTTP %d", response.StatusCode)
	}
	var manifest updateManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	if manifest.Version == "" {
		return nil, errors.New("版本信息无效")
	}
	return &manifest, nil
}

func download(client *http.Client, url string) ([]byte, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 Agent 失败: HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<20))
}
