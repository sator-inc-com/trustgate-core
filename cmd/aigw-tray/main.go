package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getlantern/systray"
)

const agentURL = "http://localhost:8787"

var (
	mStatus   *systray.MenuItem
	mStart    *systray.MenuItem
	mStop     *systray.MenuItem
	mRestart  *systray.MenuItem
	mLogs     *systray.MenuItem
	mConfig   *systray.MenuItem
	mQuit *systray.MenuItem
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("TG")
	systray.SetTooltip("TrustGate - AI Zero Trust Gateway")
	systray.SetIcon(trayIcon)

	mStatus = systray.AddMenuItem("状態: 確認中...", "Agent status")
	mStatus.Disable()

	systray.AddSeparator()

	mStart = systray.AddMenuItem("▶ サービス開始", "Start service")
	mStop = systray.AddMenuItem("■ サービス停止", "Stop service")
	mRestart = systray.AddMenuItem("↻ サービス再起動", "Restart service")

	systray.AddSeparator()

	mConfig = systray.AddMenuItem("⚙ 設定ファイルを開く", "Open config file")
	mLogs = systray.AddMenuItem("📋 最近のログ表示", "Show recent logs")

	systray.AddSeparator()

	mQuit = systray.AddMenuItem("終了", "Quit tray app")

	go healthCheckLoop()
	go handleClicks()
}

func onExit() {}

func handleClicks() {
	for {
		select {
		case <-mStart.ClickedCh:
			startAgent()
		case <-mStop.ClickedCh:
			stopAgent()
		case <-mRestart.ClickedCh:
			stopAgent()
			time.Sleep(1 * time.Second)
			startAgent()
		case <-mLogs.ClickedCh:
			showLogs()
		case <-mConfig.ClickedCh:
			openConfig()
		case <-mQuit.ClickedCh:
			systray.Quit()
		}
	}
}

func startAgent() {
	// macOS: use launchctl to manage the agent service (LaunchAgents = user level, no sudo)
	if runtime.GOOS == "darwin" {
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		svcTarget := domain + "/com.trustgate.agent"
		plist := "/Library/LaunchAgents/com.trustgate.agent.plist"

		// Try bootstrap (modern API) first, fall back to load (legacy)
		if err := exec.Command("launchctl", "bootstrap", domain, plist).Run(); err != nil {
			// Already bootstrapped — kickstart instead
			exec.Command("launchctl", "kickstart", "-k", svcTarget).Run()
		}
		notify("TrustGate", "サービスを開始しました")
		time.Sleep(2 * time.Second)
		checkHealth()
		return
	}

	aigw := findAigw()

	// Try Windows service first (sc start TrustGate)
	if runtime.GOOS == "windows" {
		scCmd := exec.Command("sc", "start", "TrustGate")
		hideCmd(scCmd)
		if err := scCmd.Run(); err == nil {
			notify("TrustGate", "サービスを開始しました")
			time.Sleep(2 * time.Second)
			checkHealth()
			return
		}
	}

	// Try aigw service start
	cmd := exec.Command(aigw, "service", "start")
	hideCmd(cmd)
	if err := cmd.Run(); err == nil {
		notify("TrustGate", "サービスを開始しました")
		time.Sleep(2 * time.Second)
		checkHealth()
		return
	}

	// Fall back to direct execution (detached so CMD window doesn't linger)
	configPath := findConfig()
	args := []string{"serve"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	agentCmd := exec.Command(aigw, args...)
	hideCmdDetached(agentCmd)
	if configPath != "" {
		agentCmd.Dir = filepath.Dir(configPath)
	}
	agentCmd.Stdout = nil
	agentCmd.Stderr = nil
	if err := agentCmd.Start(); err != nil {
		notify("TrustGate", fmt.Sprintf("起動エラー: %s\naigw: %s\nconfig: %s", err, aigw, configPath))
		return
	}
	go func() { agentCmd.Wait() }()
	notify("TrustGate", fmt.Sprintf("Agent を起動しました\n%s", aigw))
	time.Sleep(2 * time.Second)
	checkHealth()
}

func stopAgent() {
	// macOS: use launchctl to stop the agent service (LaunchAgents = user level, no sudo)
	if runtime.GOOS == "darwin" {
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		svcTarget := domain + "/com.trustgate.agent"

		// bootout (modern API) removes and stops the service
		if err := exec.Command("launchctl", "bootout", svcTarget).Run(); err != nil {
			// Fall back to legacy unload
			exec.Command("launchctl", "unload", "/Library/LaunchAgents/com.trustgate.agent.plist").Run()
		}
		// Kill any remaining process
		exec.Command("killall", "aigw").Run()
		notify("TrustGate", "サービスを停止しました")
		time.Sleep(1 * time.Second)
		checkHealth()
		return
	}

	// Try Windows service first
	if runtime.GOOS == "windows" {
		scCmd := exec.Command("sc", "stop", "TrustGate")
		hideCmd(scCmd)
		if err := scCmd.Run(); err == nil {
			notify("TrustGate", "サービスを停止しました")
			time.Sleep(1 * time.Second)
			checkHealth()
			return
		}
	}

	// Try aigw service stop
	aigw := findAigw()
	cmd := exec.Command(aigw, "service", "stop")
	hideCmd(cmd)
	if err := cmd.Run(); err == nil {
		notify("TrustGate", "サービスを停止しました")
		time.Sleep(1 * time.Second)
		checkHealth()
		return
	}

	// Fall back: kill process
	killCmd := exec.Command("taskkill", "/F", "/IM", "aigw.exe")
	hideCmd(killCmd)
	killCmd.Run()
	notify("TrustGate", "Agent を停止しました")
	time.Sleep(500 * time.Millisecond)
	checkHealth()
}

func showLogs() {
	// Find aigw binary
	aigw := findAigw()

	cmd := exec.Command(aigw, "logs", "--limit", "20", "--since", "1h")
	hideCmd(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		notify("TrustGate", "ログ取得エラー: "+err.Error())
		return
	}

	tmpFile := filepath.Join(os.TempDir(), "trustgate_logs.txt")
	os.WriteFile(tmpFile, out, 0644)

	openInEditor(tmpFile)
}

func openConfig() {
	configPath := findConfig()
	if configPath == "" {
		notify("TrustGate", "設定ファイルが見つかりません")
		return
	}
	openInEditor(configPath)
}

func openInEditor(filePath string) {
	switch runtime.GOOS {
	case "windows":
		c := exec.Command("notepad.exe", filePath)
		hideCmd(c)
		c.Start()
	case "darwin":
		exec.Command("open", "-a", "TextEdit", filePath).Start()
	default:
		exec.Command("xdg-open", filePath).Start()
	}
}

func healthCheckLoop() {
	checkHealth()
	for range time.Tick(5 * time.Second) {
		checkHealth()
	}
}

func checkHealth() {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(agentURL + "/v1/health")
	if err != nil {
		setOffline()
		return
	}
	defer resp.Body.Close()

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		setOffline()
		return
	}

	mode, _ := health["mode"].(string)

	// Count detectors if available
	detectorInfo := ""
	if detectors, ok := health["detectors"].([]interface{}); ok {
		detectorInfo = fmt.Sprintf(", %d detectors", len(detectors))
	} else if detectorCount, ok := health["detector_count"].(float64); ok {
		detectorInfo = fmt.Sprintf(", %d detectors", int(detectorCount))
	}

	mStatus.SetTitle(fmt.Sprintf("状態: ● 稼働中 (%s%s)", mode, detectorInfo))
	systray.SetTooltip("TrustGate - 稼働中")
	systray.SetIcon(trayIconGreen)

	mStart.Disable()
	mStop.Enable()
	mRestart.Enable()
	mLogs.Enable()
	mConfig.Enable()
}

func setOffline() {
	mStatus.SetTitle("状態: ○ 停止中")
	systray.SetTooltip("TrustGate - 停止中")
	systray.SetIcon(trayIconRed)

	mStart.Enable()
	mStop.Disable()
	mRestart.Disable()
	mLogs.Disable()
	mConfig.Enable() // Config can be opened even when offline
}

func notify(title, msg string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "%s"`, msg, title)).Start()
	case "windows":
		ps := fmt.Sprintf(
			`Add-Type -AssemblyName System.Windows.Forms;`+
				`$n=New-Object System.Windows.Forms.NotifyIcon;`+
				`$n.Icon=[System.Drawing.SystemIcons]::Shield;`+
				`$n.Visible=$true;`+
				`$n.ShowBalloonTip(3000,'%s','%s','Info');`+
				`Start-Sleep 4;$n.Dispose()`, title, msg)
		c := exec.Command("powershell", "-WindowStyle", "Hidden", "-Command", ps)
		hideCmd(c)
		c.Start()
	}
}

func findConfig() string {
	// Check common locations
	candidates := []string{}

	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	candidates = append(candidates, filepath.Join(dir, "agent.yaml"))

	if runtime.GOOS == "windows" {
		candidates = append(candidates, `C:\ProgramData\TrustGate\agent.yaml`)
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, "/Library/Application Support/TrustGate/agent.yaml")
	}

	// Current working directory
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "agent.yaml"))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func findAigw() string {
	// Check same directory as tray binary
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	candidate := filepath.Join(dir, "aigw")
	if runtime.GOOS == "windows" {
		candidate += ".exe"
	}
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fall back to PATH
	return "aigw"
}
