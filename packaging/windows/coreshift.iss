; CoreShift installer: the app, the coreshiftd service and the proxy cores in
; one setup exe. Built by build.ps1, which passes AppVersion (1.2.3),
; AppBuild (the commit count), AppCommit, FileTag (for the file name), Stage
; (the folder with everything to install) and OutDir.
#ifndef Stage
  #error Build with build.ps1, not by compiling this script directly
#endif
#define AppLabel AppVersion + " (сборка " + AppBuild + ")"
#define AppNumber AppVersion + "." + AppBuild

[Setup]
AppId={{8F0C2B7E-3D5A-4E61-9B2C-6A1D4F7E5C30}
AppName=CoreShift
AppVersion={#AppLabel}
AppVerName=CoreShift {#AppLabel}
VersionInfoVersion={#AppNumber}
VersionInfoProductTextVersion={#AppLabel}, {#AppCommit}
AppPublisher=CoreShift
DefaultDirName={autopf}\CoreShift
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#OutDir}
OutputBaseFilename=coreshift-setup-{#FileTag}
SetupIconFile=..\..\app\windows\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\coreshift.exe
UninstallDisplayName=CoreShift
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; The app and the service are stopped by the [Code] below.
CloseApplications=no

[Languages]
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[InstallDelete]
; Cores of an earlier version must not stay beside the new ones.
Type: filesandordirs; Name: "{app}\cores"
Type: filesandordirs; Name: "{app}\data"

[Files]
Source: "{#Stage}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Registry]
; What is installed, for the next installer to compare with.
Root: HKLM; Subkey: "Software\CoreShift"; ValueType: string; ValueName: "Version"; ValueData: "{#AppNumber}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\CoreShift"; ValueType: string; ValueName: "Label"; ValueData: "{#AppLabel}"
Root: HKLM; Subkey: "Software\CoreShift"; ValueType: string; ValueName: "Commit"; ValueData: "{#AppCommit}"

[UninstallDelete]
; Core updates leave the replaced versions (*.old) next to the cores.
Type: filesandordirs; Name: "{app}\cores"

[Icons]
Name: "{autoprograms}\CoreShift"; Filename: "{app}\coreshift.exe"
Name: "{autodesktop}\CoreShift"; Filename: "{app}\coreshift.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\coreshift.exe"; Description: "{cm:LaunchProgram,CoreShift}"; Flags: nowait postinstall skipifsilent runasoriginaluser

[Code]
var
  // The installed version, "0.2.0.14" and "0.2.0 (сборка 14)"; empty on a
  // first install.
  PrevNumber, PrevLabel: String;

// Takes the next dot-separated number off S.
function NextPart(var S: String): Integer;
var
  P: Integer;
begin
  P := Pos('.', S);
  if P = 0 then
  begin
    Result := StrToIntDef(S, 0);
    S := '';
  end else
  begin
    Result := StrToIntDef(Copy(S, 1, P - 1), 0);
    Delete(S, 1, P);
  end;
end;

// Compares "1.2.3.4" versions: 1 when A is later, -1 when earlier, else 0.
function CompareVersions(A, B: String): Integer;
var
  I, X, Y: Integer;
begin
  Result := 0;
  for I := 1 to 4 do
  begin
    X := NextPart(A);
    Y := NextPart(B);
    if X <> Y then
    begin
      if X > Y then Result := 1 else Result := -1;
      Exit;
    end;
  end;
end;

function InitializeSetup: Boolean;
begin
  Result := True;
  if RegQueryStringValue(HKLM64, 'Software\CoreShift', 'Version', PrevNumber) then
  begin
    if not RegQueryStringValue(HKLM64, 'Software\CoreShift', 'Label', PrevLabel) then
      PrevLabel := PrevNumber;
  end
  // 0.1.0 kept its version only in the uninstall entry.
  else if RegQueryStringValue(HKLM64, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{8F0C2B7E-3D5A-4E61-9B2C-6A1D4F7E5C30}_is1',
      'DisplayVersion', PrevLabel) then
    PrevNumber := PrevLabel;
  if (PrevNumber <> '') and (CompareVersions(PrevNumber, '{#AppNumber}') > 0) then
    Result := SuppressibleMsgBox('Сейчас установлена более новая версия CoreShift: ' + PrevLabel + '.' + #13#10#13#10 +
      'Установить более раннюю версию {#AppLabel}? Настройки и подписки сохранятся.',
      mbConfirmation, MB_YESNO, IDYES) = IDYES;
end;

// The "ready to install" page says which version replaces which.
function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
begin
  if PrevLabel = '' then
    Result := 'Версия:' + NewLine + Space + '{#AppLabel}'
  else if PrevLabel = '{#AppLabel}' then
    Result := 'Версия:' + NewLine + Space + '{#AppLabel} (переустановка)'
  else
    Result := 'Версия:' + NewLine + Space + 'сейчас ' + PrevLabel + NewLine + Space + 'будет {#AppLabel}';
  if MemoDirInfo <> '' then
    Result := Result + NewLine + NewLine + MemoDirInfo;
  if MemoTasksInfo <> '' then
    Result := Result + NewLine + NewLine + MemoTasksInfo;
end;

// Runs coreshiftd with its output kept, so that a failure can be shown.
function Daemon(const Exe, Params: String; var Output: String): Boolean;
var
  Log: String;
  Text: AnsiString;
  Code: Integer;
begin
  Log := ExpandConstant('{tmp}\coreshiftd-output.txt');
  DeleteFile(Log);
  Result := Exec(ExpandConstant('{cmd}'), '/C ""' + Exe + '" ' + Params + ' > "' + Log + '" 2>&1"',
    '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
  Output := '';
  if LoadStringFromFile(Log, Text) then
    Output := Trim(String(Text));
end;

function ServiceExists: Boolean;
var
  Code: Integer;
begin
  Result := Exec(ExpandConstant('{sys}\sc.exe'), 'query CoreShift', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
end;

// The app only shows the service's state; closing it touches no connection.
procedure CloseApp;
var
  Code: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM coreshift.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Output: String;
begin
  Result := '';
  CloseApp;
  // Remove the service of an earlier install, wherever it ran from. Stopping
  // it disconnects the VPN and restores DNS.
  if ServiceExists then
  begin
    ExtractTemporaryFile('coreshiftd.exe');
    if not Daemon(ExpandConstant('{tmp}\coreshiftd.exe'), 'service uninstall', Output) then
      Result := 'Не удалось остановить службу CoreShift предыдущей версии:' + #13#10 + Output;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Output: String;
begin
  if CurStep <> ssPostInstall then
    Exit;
  if not Daemon(ExpandConstant('{app}\coreshiftd.exe'), 'service install', Output) or
     not Daemon(ExpandConstant('{app}\coreshiftd.exe'), 'service start', Output) then
    SuppressibleMsgBox('Служба CoreShift не запустилась, без неё VPN не будет работать:' + #13#10#13#10 + Output,
      mbError, MB_OK, IDOK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Code: Integer;
  Data: String;
begin
  case CurUninstallStep of
    usUninstall:
      begin
        CloseApp;
        Exec(ExpandConstant('{app}\coreshiftd.exe'), 'service uninstall', '', SW_HIDE, ewWaitUntilTerminated, Code);
        // In case the service could not undo its DNS changes itself.
        Exec(ExpandConstant('{app}\coreshiftd.exe'), 'dns recover', '', SW_HIDE, ewWaitUntilTerminated, Code);
      end;
    usPostUninstall:
      begin
        Data := ExpandConstant('{commonappdata}\CoreShift');
        if DirExists(Data) and (SuppressibleMsgBox('Удалить также настройки и подписки CoreShift?',
            mbConfirmation, MB_YESNO, IDNO) = IDYES) then
          DelTree(Data, True, True, True);
      end;
  end;
end;
