; CoreShift installer: the app, the coreshiftd service and the proxy cores in
; one setup exe. Built by build.ps1, which passes AppVersion (1.2.3),
; AppBuild (the commit count), AppCommit, FileTag (for the file name), Stage
; (the folder with everything to install) and OutDir.
#ifndef Stage
  #error Build with build.ps1, not by compiling this script directly
#endif
#define AppLabel AppVersion
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
; Any folder: the page is shown on updates too, with the current one.
DisableDirPage=no
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
; coreshift:// links, which subscription panels' "add to app" buttons open.
Root: HKLM; Subkey: "Software\Classes\coreshift"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\coreshift"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKLM; Subkey: "Software\Classes\coreshift\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"",0"
Root: HKLM; Subkey: "Software\Classes\coreshift\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""
; The links of other clients, only where no program of their own takes them.
Root: HKLM; Subkey: "Software\Classes\happ"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey; Check: SchemeFree('happ')
Root: HKLM; Subkey: "Software\Classes\happ"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Check: SchemeFree('happ')
Root: HKLM; Subkey: "Software\Classes\happ\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""; Check: SchemeFree('happ')
Root: HKLM; Subkey: "Software\Classes\hiddify"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey; Check: SchemeFree('hiddify')
Root: HKLM; Subkey: "Software\Classes\hiddify"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Check: SchemeFree('hiddify')
Root: HKLM; Subkey: "Software\Classes\hiddify\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""; Check: SchemeFree('hiddify')
Root: HKLM; Subkey: "Software\Classes\clash"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey; Check: SchemeFree('clash')
Root: HKLM; Subkey: "Software\Classes\clash"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Check: SchemeFree('clash')
Root: HKLM; Subkey: "Software\Classes\clash\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""; Check: SchemeFree('clash')
Root: HKLM; Subkey: "Software\Classes\sing-box"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey; Check: SchemeFree('sing-box')
Root: HKLM; Subkey: "Software\Classes\sing-box"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Check: SchemeFree('sing-box')
Root: HKLM; Subkey: "Software\Classes\sing-box\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""; Check: SchemeFree('sing-box')
Root: HKLM; Subkey: "Software\Classes\v2raytun"; ValueType: string; ValueName: ""; ValueData: "URL:CoreShift"; Flags: uninsdeletekey; Check: SchemeFree('v2raytun')
Root: HKLM; Subkey: "Software\Classes\v2raytun"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Check: SchemeFree('v2raytun')
Root: HKLM; Subkey: "Software\Classes\v2raytun\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\coreshift.exe"" ""%1"""; Check: SchemeFree('v2raytun')

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
  // The installed version, "0.2.0.14" and "0.2.0" (older installers wrote
  // "0.2.0 (сборка 14)"); empty on a first install.
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

// Whether CoreShift may take another client's links: no program handles
// them, or CoreShift already does (an earlier install, maybe elsewhere).
function SchemeFree(Scheme: String): Boolean;
var
  Cmd: String;
begin
  Result := not RegKeyExists(HKCR, Scheme) or
    (RegQueryStringValue(HKCR, Scheme + '\shell\open\command', '', Cmd) and (Pos('coreshift.exe', Lowercase(Cmd)) > 0));
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
// The console speaks UTF-8 meanwhile, so cmd's own messages ("access
// denied") read like coreshiftd's.
function Daemon(const Exe, Params: String; var Output: String): Boolean;
var
  Log: String;
  Text: AnsiString;
  Code: Integer;
begin
  Log := ExpandConstant('{tmp}\coreshiftd-output.txt');
  DeleteFile(Log);
  Result := Exec(ExpandConstant('{cmd}'), '/C "chcp 65001 >nul & "' + Exe + '" ' + Params + ' > "' + Log + '" 2>&1"',
    '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
  Output := '';
  if LoadStringFromFile(Log, Text) then
    Output := Trim(UTF8Decode(Text));
end;

function ServiceExists: Boolean;
var
  Code: Integer;
begin
  Result := Exec(ExpandConstant('{sys}\sc.exe'), 'query CoreShift', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
end;

// Closing the app stops the service after a moment, and the VPN with it;
// the service is removed right after anyway.
procedure CloseApp;
var
  Code: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM coreshift.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

// The folder must be CoreShift's own: its files are replaced and removed
// as a whole, and it is closed to everyone but administrators below.
function NextButtonClick(CurPageID: Integer): Boolean;
var
  Dir: String;
  Rec: TFindRec;
  Empty: Boolean;
begin
  Result := True;
  if CurPageID <> wpSelectDir then
    Exit;
  Dir := RemoveBackslashUnlessRoot(WizardDirValue);
  if Length(Dir) <= 3 then
  begin
    MsgBox('Выберите папку, а не корень диска, например ' + Dir + '\CoreShift.', mbError, MB_OK);
    Result := False;
    Exit;
  end;
  if not DirExists(Dir) or FileExists(Dir + '\coreshift.exe') then
    Exit;
  Empty := True;
  if FindFirst(Dir + '\*', Rec) then
  try
    repeat
      if (Rec.Name <> '.') and (Rec.Name <> '..') then
        Empty := False;
    until not Empty or not FindNext(Rec);
  finally
    FindClose(Rec);
  end;
  if not Empty then
  begin
    MsgBox('В папке ' + Dir + ' уже есть другие файлы. Выберите пустую или новую папку, например ' + Dir + '\CoreShift.', mbError, MB_OK);
    Result := False;
  end;
end;

// The service runs as SYSTEM from this folder: outside Program Files it
// could inherit write access for users, who could then replace its files.
// Only administrators and SYSTEM may change them: the folder and everything
// in it belong to Administrators (an owner may always change permissions,
// so an empty folder a user made beforehand must not stay theirs), the
// folder gets its own permissions, and everything in it takes them from
// the folder. Done before the files are copied as well as after, so the
// folder is never open to users while they arrive; this also gives the
// files of 0.4.0 betas, which had none, their permissions back.
procedure LockDownAppDir;
var
  Code: Integer;
begin
  ForceDirectories(ExpandConstant('{app}'));
  Exec(ExpandConstant('{sys}\icacls.exe'), '"' + ExpandConstant('{app}') + '" /setowner *S-1-5-32-544 /T /C /Q',
    '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\icacls.exe'), '"' + ExpandConstant('{app}') + '" /inheritance:r /grant:r *S-1-5-18:(OI)(CI)F *S-1-5-32-544:(OI)(CI)F *S-1-5-32-545:(OI)(CI)RX /C /Q',
    '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\icacls.exe'), '"' + ExpandConstant('{app}') + '\*" /reset /T /C /Q',
    '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Output: String;
begin
  Result := '';
  CloseApp;
  LockDownAppDir;
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
  LockDownAppDir;
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
        // "Автозапуск", which the app sets for the user.
        RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'CoreShift');
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
