; TrustGate for Workforce - Inno Setup Installer
; Compile with: ISCC.exe installer.iss

#define MyAppName "TrustGate"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "TrustGate"
#define MyAppURL "https://github.com/yosuke-seno/trustgate"

[Setup]
AppId={{E8A3B2C1-4D5F-6789-ABCD-EF0123456789}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
OutputBaseFilename=TrustGate-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
LicenseFile=..\LICENSE
SetupIconFile=setup-icon.ico
UninstallDisplayIcon={app}\aigw-tray.exe,0
MinVersion=10.0

[Languages]
Name: "japanese"; MessagesFile: "compiler:Languages\Japanese.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Messages]
japanese.WelcomeLabel2=TrustGate for Workforce をインストールします。%n%nAI SaaS利用（ChatGPT, Gemini, Claude.ai等）のセキュリティ統制を行うエージェントです。%n%nインストールを続行するには「次へ」をクリックしてください。
english.WelcomeLabel2=This will install TrustGate for Workforce on your computer.%n%nTrustGate is an AI Zero Trust Gateway that inspects and controls AI SaaS usage (ChatGPT, Gemini, Claude.ai, etc.).%n%nClick Next to continue.

[Types]
Name: "full"; Description: "フルインストール（推奨）"
Name: "agent"; Description: "エージェントのみ"
Name: "custom"; Description: "カスタム"; Flags: iscustom

[Components]
Name: "agent"; Description: "TrustGate Agent"; Types: full agent custom; Flags: fixed
Name: "config"; Description: "デフォルト設定ファイル"; Types: full agent
Name: "extension"; Description: "Chrome/Edge ブラウザ拡張"; Types: full
Name: "service"; Description: "Windows Service として登録"; Types: full

[Files]
; Agent binary
Source: "..\dist\windows\aigw.exe"; DestDir: "{app}"; DestName: "aigw.exe"; Flags: ignoreversion; Components: agent
; Tray manager
Source: "..\dist\windows\aigw-tray.exe"; DestDir: "{app}"; DestName: "aigw-tray.exe"; Flags: ignoreversion; Components: agent

; Config files
Source: "default-agent.yaml"; DestDir: "{commonappdata}\TrustGate"; DestName: "agent.yaml"; Flags: onlyifdoesntexist confirmoverwrite; Components: config
Source: "default-policies.yaml"; DestDir: "{commonappdata}\TrustGate"; DestName: "policies.yaml"; Flags: onlyifdoesntexist confirmoverwrite; Components: config

; Browser extension
Source: "..\extension\*"; DestDir: "{commonappdata}\TrustGate\extension"; Flags: ignoreversion recursesubdirs; Components: extension

; Icon file
Source: "setup-icon.ico"; DestDir: "{app}"; DestName: "trustgate.ico"; Flags: ignoreversion

[Dirs]
Name: "{commonappdata}\TrustGate"; Permissions: users-modify
Name: "{commonappdata}\TrustGate\logs"

[Icons]
Name: "{group}\TrustGate"; Filename: "{app}\aigw-tray.exe"; IconFilename: "{app}\trustgate.ico"; Comment: "TrustGate トレイマネージャー"
Name: "{group}\アンインストール"; Filename: "{uninstallexe}"

[Registry]
; Auto-start tray app on login
Root: HKCU; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "TrustGate"; ValueData: """{app}\aigw-tray.exe"""; Flags: uninsdeletevalue

[Registry]
; Add to PATH
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath('{app}')

; Chrome extension (force install via registry - enterprise deployment)
Root: HKLM; Subkey: "SOFTWARE\Policies\Google\Chrome\ExtensionInstallForceList"; ValueType: string; ValueName: "1"; ValueData: "trustgate-extension-id;{commonappdata}\TrustGate\extension"; Components: extension; Flags: uninsdeletevalue

; Edge extension
Root: HKLM; Subkey: "SOFTWARE\Policies\Microsoft\Edge\ExtensionInstallForceList"; ValueType: string; ValueName: "1"; ValueData: "trustgate-extension-id;{commonappdata}\TrustGate\extension"; Components: extension; Flags: uninsdeletevalue

[Run]
; Install and start service
Filename: "{app}\aigw.exe"; Parameters: "service install --config ""{commonappdata}\TrustGate\agent.yaml"""; StatusMsg: "Windows Serviceを登録中..."; Flags: runhidden; Components: service
Filename: "{app}\aigw.exe"; Parameters: "service start"; StatusMsg: "サービスを開始中..."; Flags: runhidden; Components: service

; Configure service recovery policy: restart on failure
Filename: "sc.exe"; Parameters: "failure TrustGate reset= 86400 actions= restart/5000/restart/10000/restart/30000"; StatusMsg: "サービス復旧ポリシーを設定中..."; Flags: runhidden; Components: service

; Post-install actions
Filename: "{app}\aigw-tray.exe"; Description: "TrustGate トレイマネージャーを起動"; Flags: postinstall nowait skipifsilent

[UninstallRun]
; Kill tray app before uninstalling
Filename: "taskkill.exe"; Parameters: "/F /IM aigw-tray.exe"; Flags: runhidden
; Stop and uninstall service
Filename: "{app}\aigw.exe"; Parameters: "service stop"; Flags: runhidden
Filename: "{app}\aigw.exe"; Parameters: "service uninstall"; Flags: runhidden

[Code]
var
  ModePage: TWizardPage;
  ConfigPage: TWizardPage;
  ModeRadioStandalone: TNewRadioButton;
  ModeRadioManaged: TNewRadioButton;
  ServerURLEdit: TNewEdit;
  ApiKeyEdit: TNewEdit;

procedure InitializeWizard;
var
  LabelMode, LabelStandaloneDesc, LabelManagedDesc: TNewStaticText;
  LabelURL, LabelURLHelp, LabelKey, LabelKeyHelp: TNewStaticText;
begin
  { === Page 1: Mode Selection === }
  ModePage := CreateCustomPage(wpSelectComponents,
    '動作モードの選択',
    'TrustGate Agent の動作モードを選択してください。');

  ModeRadioStandalone := TNewRadioButton.Create(ModePage);
  ModeRadioStandalone.Parent := ModePage.Surface;
  ModeRadioStandalone.Caption := 'Standalone（単体動作）';
  ModeRadioStandalone.Top := ScaleY(0);
  ModeRadioStandalone.Left := 0;
  ModeRadioStandalone.Width := ModePage.SurfaceWidth;
  ModeRadioStandalone.Height := ScaleY(20);
  ModeRadioStandalone.Checked := True;

  LabelStandaloneDesc := TNewStaticText.Create(ModePage);
  LabelStandaloneDesc.Parent := ModePage.Surface;
  LabelStandaloneDesc.Caption := 'Control Plane なし。ポリシーはローカル管理。';
  LabelStandaloneDesc.Top := ScaleY(22);
  LabelStandaloneDesc.Left := ScaleX(24);
  LabelStandaloneDesc.Font.Color := clGray;

  ModeRadioManaged := TNewRadioButton.Create(ModePage);
  ModeRadioManaged.Parent := ModePage.Surface;
  ModeRadioManaged.Caption := 'Managed（CP接続）';
  ModeRadioManaged.Top := ScaleY(56);
  ModeRadioManaged.Left := 0;
  ModeRadioManaged.Width := ModePage.SurfaceWidth;
  ModeRadioManaged.Height := ScaleY(20);

  LabelManagedDesc := TNewStaticText.Create(ModePage);
  LabelManagedDesc.Parent := ModePage.Surface;
  LabelManagedDesc.Caption := 'ポリシー配信・レポート・Agent管理。';
  LabelManagedDesc.Top := ScaleY(78);
  LabelManagedDesc.Left := ScaleX(24);
  LabelManagedDesc.Font.Color := clGray;

  { === Page 2: CP Config (Managed only) === }
  ConfigPage := CreateCustomPage(ModePage.ID,
    'Control Plane 接続設定',
    'Control Plane の URL と API Key を入力してください。');

  LabelURL := TNewStaticText.Create(ConfigPage);
  LabelURL.Parent := ConfigPage.Surface;
  LabelURL.Caption := 'Control Plane URL:';
  LabelURL.Top := ScaleY(0);
  LabelURL.Left := 0;

  ServerURLEdit := TNewEdit.Create(ConfigPage);
  ServerURLEdit.Parent := ConfigPage.Surface;
  ServerURLEdit.Top := ScaleY(20);
  ServerURLEdit.Left := 0;
  ServerURLEdit.Width := ConfigPage.SurfaceWidth;
  ServerURLEdit.Text := ExpandConstant('{param:SERVERURL|http://localhost:9090}');

  LabelURLHelp := TNewStaticText.Create(ConfigPage);
  LabelURLHelp.Parent := ConfigPage.Surface;
  LabelURLHelp.Caption := '例: http://cp.company.com:9090';
  LabelURLHelp.Top := ScaleY(46);
  LabelURLHelp.Left := 0;
  LabelURLHelp.Font.Color := clGray;

  LabelKey := TNewStaticText.Create(ConfigPage);
  LabelKey.Parent := ConfigPage.Surface;
  LabelKey.Caption := 'API Key:';
  LabelKey.Top := ScaleY(80);
  LabelKey.Left := 0;

  ApiKeyEdit := TNewEdit.Create(ConfigPage);
  ApiKeyEdit.Parent := ConfigPage.Surface;
  ApiKeyEdit.Top := ScaleY(100);
  ApiKeyEdit.Left := 0;
  ApiKeyEdit.Width := ConfigPage.SurfaceWidth;
  ApiKeyEdit.Text := ExpandConstant('{param:APIKEY|}');

  LabelKeyHelp := TNewStaticText.Create(ConfigPage);
  LabelKeyHelp.Parent := ConfigPage.Surface;
  LabelKeyHelp.Caption := 'Dashboard > Agent Setup から取得';
  LabelKeyHelp.Top := ScaleY(126);
  LabelKeyHelp.Left := 0;
  LabelKeyHelp.Font.Color := clGray;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  { Skip CP config page if Standalone mode is selected }
  if (PageID = ConfigPage.ID) and ModeRadioStandalone.Checked then
    Result := True;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = ConfigPage.ID then
  begin
    if Trim(ApiKeyEdit.Text) = '' then
    begin
      MsgBox('API Key を入力してください。' + #13#10 +
             'Control Plane の Dashboard > Agent Setup から確認できます。', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

procedure GenerateAgentYaml;
var
  Lines: TStringList;
  ConfigDir: string;
begin
  ConfigDir := ExpandConstant('{commonappdata}\TrustGate');
  Lines := TStringList.Create;
  try
    Lines.Add('version: "1"');

    if ModeRadioManaged.Checked then
    begin
      Lines.Add('mode: managed');
      Lines.Add('');
      Lines.Add('sync:');
      Lines.Add('  server_url: ' + Trim(ServerURLEdit.Text));
      Lines.Add('  api_key: "' + Trim(ApiKeyEdit.Text) + '"');
      Lines.Add('  heartbeat_sec: 30');
      Lines.Add('  policy_pull_sec: 60');
      Lines.Add('  stats_push_sec: 60');
    end
    else
    begin
      Lines.Add('mode: standalone');
    end;

    Lines.Add('');
    Lines.Add('listen:');
    Lines.Add('  host: 127.0.0.1');
    Lines.Add('  port: 8787');
    Lines.Add('');
    Lines.Add('detectors:');
    Lines.Add('  pii:');
    Lines.Add('    enabled: true');
    Lines.Add('  injection:');
    Lines.Add('    enabled: true');
    Lines.Add('    language: [en, ja]');
    Lines.Add('  confidential:');
    Lines.Add('    enabled: true');
    Lines.Add('    keywords:');
    Lines.Add('      critical: ["極秘", "社外秘", "CONFIDENTIAL", "TOP SECRET"]');
    Lines.Add('      high: ["機密", "内部限定", "INTERNAL ONLY"]');
    Lines.Add('');
    Lines.Add('policy:');
    Lines.Add('  source: local');
    Lines.Add('  file: policies.yaml');
    Lines.Add('');

    if ModeRadioStandalone.Checked then
    begin
      Lines.Add('audit:');
      Lines.Add('  mode: memory');
      Lines.Add('  max_entries: 100');
    end
    else
    begin
      Lines.Add('audit:');
      Lines.Add('  mode: wal');
      Lines.Add('  path: audit_data');
    end;

    Lines.Add('');
    Lines.Add('logging:');
    Lines.Add('  level: info');
    Lines.Add('  format: text');

    Lines.SaveToFile(ConfigDir + '\agent.yaml');
  finally
    Lines.Free;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    if Trim(ApiKeyEdit.Text) <> '' then
    begin
      GenerateAgentYaml;
    end;
  end;
end;

function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', OrigPath)
  then begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Param + ';', ';' + OrigPath + ';') = 0;
end;

function IsPortInUse(Port: Integer): Boolean;
var
  ResultCode: Integer;
begin
  // Use netstat to check if the port is already in use
  Exec('cmd.exe', '/C netstat -an | findstr :' + IntToStr(Port) + ' | findstr LISTENING > nul 2>&1', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := (ResultCode = 0);
end;

function IsUpgradeInstall(): Boolean;
var
  InstalledVersion: string;
begin
  Result := RegQueryStringValue(HKLM, 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{E8A3B2C1-4D5F-6789-ABCD-EF0123456789}_is1', 'DisplayVersion', InstalledVersion);
end;

function InitializeSetup(): Boolean;
var
  ResultCode: Integer;
begin
  Result := True;

  // Upgrade detection: stop existing service before upgrade
  if IsUpgradeInstall() then
  begin
    // Kill tray app
    Exec('taskkill.exe', '/F /IM aigw-tray.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    // Stop service
    Exec('sc.exe', 'stop TrustGate', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    // Wait for service to stop
    Sleep(2000);
  end;

  // Port check: warn if port 8787 is already in use by another process
  if IsPortInUse(8787) then
  begin
    if not IsUpgradeInstall() then
    begin
      if MsgBox('ポート 8787 は既に使用されています。' + #13#10 +
                '別のアプリケーションがこのポートを使用している可能性があります。' + #13#10 + #13#10 +
                'インストールを続行しますか？', mbConfirmation, MB_YESNO) = IDNO then
      begin
        Result := False;
      end;
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  OrigPath: string;
  NewPath: string;
  AppDir: string;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    AppDir := ExpandConstant('{app}');
    if RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', OrigPath) then
    begin
      NewPath := OrigPath;
      StringChangeEx(NewPath, ';' + AppDir, '', True);
      RegWriteExpandStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', NewPath);
    end;
  end;
end;
