# Homebrew formula for TrustGate Agent
# Install: brew install trustgate/tap/aigw
# Usage after install:
#   brew services start aigw    # Start as background service
#   aigw serve --mock-backend   # Start manually with mock backend
#   aigw doctor                 # Diagnose environment

class Aigw < Formula
  desc "AI Zero Trust Gateway — inspects and controls AI input/output in real-time"
  homepage "https://github.com/yosuke-seno/trustgate"
  license "BSL-1.1"

  # TODO: Update URL and SHA256 for each release
  # url "https://github.com/yosuke-seno/trustgate/archive/refs/tags/v0.1.0.tar.gz"
  # sha256 "TODO"
  head "https://github.com/yosuke-seno/trustgate.git", branch: "main"

  depends_on "go" => :build
  depends_on "onnxruntime" => :optional

  def install
    # Build agent binary
    system "go", "build",
           "-ldflags", "-X main.version=#{version}",
           "-o", bin/"aigw",
           "./cmd/aigw"

    # Build tray manager
    system "go", "build",
           "-ldflags", "-X main.version=#{version}",
           "-o", bin/"aigw-tray",
           "./cmd/aigw-tray"

    # Install default configs
    etc_trustgate = etc/"trustgate"
    etc_trustgate.install "scripts/default-agent.yaml" => "agent.yaml" unless (etc_trustgate/"agent.yaml").exist?
    etc_trustgate.install "scripts/default-policies.yaml" => "policies.yaml" unless (etc_trustgate/"policies.yaml").exist?

    # Install launchd plists
    (buildpath/"scripts/com.trustgate.agent.plist").write plist_agent
  end

  def plist_agent
    <<~EOS
      <?xml version="1.0" encoding="UTF-8"?>
      <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
      <plist version="1.0">
      <dict>
          <key>Label</key>
          <string>com.trustgate.agent</string>
          <key>ProgramArguments</key>
          <array>
              <string>#{opt_bin}/aigw</string>
              <string>serve</string>
              <string>--config</string>
              <string>#{etc}/trustgate/agent.yaml</string>
          </array>
          <key>RunAtLoad</key>
          <true/>
          <key>KeepAlive</key>
          <dict>
              <key>SuccessfulExit</key>
              <false/>
          </dict>
          <key>WorkingDirectory</key>
          <string>#{etc}/trustgate</string>
          <key>StandardOutPath</key>
          <string>#{var}/log/trustgate.log</string>
          <key>StandardErrorPath</key>
          <string>#{var}/log/trustgate.err</string>
      </dict>
      </plist>
    EOS
  end

  service do
    run [opt_bin/"aigw", "serve", "--config", etc/"trustgate/agent.yaml"]
    working_dir etc/"trustgate"
    log_path var/"log/trustgate.log"
    error_log_path var/"log/trustgate.err"
    keep_alive crashed: true
  end

  def caveats
    <<~EOS
      TrustGate AI Zero Trust Gateway has been installed.

      Configuration:
        #{etc}/trustgate/agent.yaml
        #{etc}/trustgate/policies.yaml

      Start as a service:
        brew services start aigw

      Or run manually:
        aigw serve --mock-backend

      Download LLM model for Stage 2 detection:
        aigw model download prompt-guard-2-86m

      Test:
        curl http://localhost:8787/v1/health
    EOS
  end

  test do
    assert_match "version", shell_output("#{bin}/aigw --version", 0)
  end
end
