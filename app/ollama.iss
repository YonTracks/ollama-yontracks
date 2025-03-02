; Inno Setup Installer for Ollama
;
; To build the installer use the build script invoked from the top of the source tree
; 
; powershell -ExecutionPolicy Bypass -File .\scripts\build_windows.ps

#define MyAppName "Ollama"
#if GetEnv("PKG_VERSION") != ""
  #define MyAppVersion GetEnv("PKG_VERSION")
#else
  #define MyAppVersion "0.0.0"
#endif
#define MyAppPublisher "Ollama"
#define MyAppURL "https://ollama.com/"
#define MyAppExeName "ollama app.exe"
#define MyIcon ".\assets\app.ico"

[Setup]
; NOTE: The value of AppId uniquely identifies this application. Do not use the same AppId value in installers for other applications.
; (To generate a new GUID, click Tools | Generate GUID inside the IDE.)
AppId={{44E83376-CE68-45EB-8FC1-393500EB558C}}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
VersionInfoVersion={#MyAppVersion}
;AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
ArchitecturesAllowed=x64compatible arm64
ArchitecturesInstallIn64BitMode=x64compatible arm64
DefaultDirName={localappdata}\Programs\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputBaseFilename="OllamaSetup"
SetupIconFile={#MyIcon}
UninstallDisplayIcon={uninstallexe}
; RunOnceId warning disabled
MissingRunOnceIdsWarning=no

Compression=lzma2
SolidCompression=no
WizardStyle=modern
ChangesEnvironment=yes
OutputDir=..\dist\

; Disable logging once everything's battle tested
; Filename will be %TEMP%\Setup Log*.txt
SetupLogging=yes
CloseApplications=yes
RestartApplications=no
RestartIfNeededByRun=no

; https://jrsoftware.org/ishelp/index.php?topic=setup_wizardimagefile
WizardSmallImageFile=.\assets\setup.bmp

; Ollama requires Windows 10 22H2 or newer for proper unicode rendering
; TODO: consider setting this to 10.0.19045
MinVersion=10.0.10240

; First release that supports WinRT UI Composition for win32 apps
; MinVersion=10.0.17134
; First release with XAML Islands - possible UI path forward
; MinVersion=10.0.18362

; quiet...
DisableDirPage=yes
DisableFinishedPage=yes
DisableReadyMemo=yes
DisableReadyPage=yes
DisableStartupPrompt=yes
DisableWelcomePage=yes

; Larger DialogFontSize will auto size the wizard window accordingly.
WizardSizePercent=100

#if GetEnv("KEY_CONTAINER")
SignTool=MySignTool
SignedUninstaller=yes
#endif

SetupMutex=OllamaSetupMutex

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

; Default=8
; [LangOptions]
; DialogFontSize=12

[Files]
#if DirExists("..\dist\windows-amd64")
Source: "..\dist\windows-amd64-app.exe"; DestDir: "{app}"; DestName: "{#MyAppExeName}" ;Check: not IsArm64(); Flags: ignoreversion 64bit
Source: "..\dist\windows-amd64\ollama.exe"; DestDir: "{app}"; Check: not IsArm64(); Flags: ignoreversion 64bit
Source: "..\dist\windows-amd64\lib\ollama\*"; DestDir: "{app}\lib\ollama\"; Check: not IsArm64(); Flags: ignoreversion 64bit recursesubdirs
#endif

#if DirExists("..\dist\windows-arm64")
Source: "..\dist\windows-arm64\vc_redist.arm64.exe"; DestDir: "{tmp}"; Check: IsArm64() and vc_redist_needed(); Flags: deleteafterinstall
Source: "..\dist\windows-arm64-app.exe"; DestDir: "{app}"; DestName: "{#MyAppExeName}" ;Check: IsArm64(); Flags: ignoreversion 64bit
Source: "..\dist\windows-arm64\ollama.exe"; DestDir: "{app}"; Check: IsArm64(); Flags: ignoreversion 64bit
#endif

Source: "..\dist\ollama_welcome.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: ".\assets\app.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\app.ico"
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\app.ico"
Name: "{userprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\app.ico"

[Run]
#if DirExists("..\dist\windows-arm64")
Filename: "{tmp}\vc_redist.arm64.exe"; Parameters: "/install /passive /norestart"; Check: IsArm64() and vc_redist_needed(); StatusMsg: "Installing VC++ Redistributables..."; Flags: waituntilterminated
#endif
Filename: "{cmd}"; Parameters: "/C set PATH={app};%PATH% & ""{app}\{#MyAppExeName}"""; Flags: postinstall nowait runhidden

[UninstallRun]
; Filename: "{cmd}"; Parameters: "/C ""taskkill /im ''{#MyAppExeName}'' /f /t"; Flags: runhidden
; Filename: "{cmd}"; Parameters: "/C ""taskkill /im ollama.exe /f /t"; Flags: runhidden
; Each command is assigned a RunOnceId so that it runs only once.
Filename: "taskkill"; Parameters: "/im ""{#MyAppExeName}"" /f /t"; Flags: runhidden; RunOnceId: "KillMyAppExe"
Filename: "taskkill"; Parameters: "/im ""ollama.exe"" /f /t"; Flags: runhidden; RunOnceId: "KillOllamaExe"


; HACK!  need to give the server and app enough time to exit
; TODO - convert this to a Pascal code script so it waits until they're no longer running, then completes
Filename: "{cmd}"; Parameters: "/c timeout 5"; Flags: runhidden

[UninstallDelete]
Type: filesandordirs; Name: "{%TEMP}\ollama*"
; The following folders are now handled in code:
; Type: filesandordirs; Name: "{%LOCALAPPDATA}\Ollama"
; Type: filesandordirs; Name: "{%LOCALAPPDATA}\Programs\Ollama"
; Type: filesandordirs; Name: "{%USERPROFILE}\.ollama\models"
; Type: filesandordirs; Name: "{%USERPROFILE}\.ollama\history"

[InstallDelete]
Type: filesandordirs; Name: "{%TEMP}\ollama*"
Type: filesandordirs; Name: "{%LOCALAPPDATA}\Programs\Ollama"

[Messages]
WizardReady=Ollama
ReadyLabel1=%nLet's get you up and running with your own large language models.
SetupAppRunningError=Another Ollama installer is running.%n%nPlease cancel or finish the other installer, then click OK to continue with this install, or Cancel to exit.

[Registry]
Root: HKCU; Subkey: "Environment"; \
    ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; \
    Check: NeedsAddPath('{app}')

[Code]
const
  wpUninstallConfirm = 1;  { Define the uninstall confirmation page identifier }
  MY_FILE_ATTRIBUTE_DIRECTORY = $10;

var
  UserWantsKeep: Boolean;       { Choice for %USERPROFILE%\.ollama }
  UserWantsKeepLocal: Boolean;  { Choice for %LOCALAPPDATA%\Ollama }

{ Recursive function to delete all files and subdirectories in a given directory }
function DeleteDirectoryRecursive(const Dir: string): Boolean;
var
  FindRec: TFindRec;
  FilePath: string;
begin
  Result := True;
  if not DirExists(Dir) then
    Exit;
  if FindFirst(AddBackslash(Dir) + '*', FindRec) then
  begin
    try
      repeat
        if (FindRec.Name <> '.') and (FindRec.Name <> '..') then
        begin
          FilePath := AddBackslash(Dir) + FindRec.Name;
          if (FindRec.Attributes and MY_FILE_ATTRIBUTE_DIRECTORY) <> 0 then
          begin
            if not DeleteDirectoryRecursive(FilePath) then
              Result := False;
          end
          else
          begin
            if not DeleteFile(FilePath) then
              Result := False;
          end;
        end;
      until not FindNext(FindRec);
    finally
      FindClose(FindRec);
    end;
  end;
  if not RemoveDir(Dir) then
    Result := False;
end;

{ InitializeUninstall:
  Ask the user whether to keep their models/configuration (in %USERPROFILE%\.ollama)
  and whether to keep logs/updates (in %LOCALAPPDATA%\Ollama).
  The decisions are stored in UserWantsKeep and UserWantsKeepLocal respectively.
  Deletion for folders chosen for removal is deferred until after final confirmation. }
function InitializeUninstall(): Boolean;
begin
  UserWantsKeep := MsgBox(
    'Uninstall is initializing.'#13#10 +
    'Do you want to keep your models, history, and configuration files? '#13#10 +
    'Choose Yes to keep (the %USERPROFILE%\.ollama folder), or No to delete it.',
    mbConfirmation, MB_YESNO) = idYes;

  if UserWantsKeep then
    MsgBox('The .ollama folder (models, history, configuration) will be kept.', mbInformation, MB_OK)
  else
    MsgBox('The .ollama folder will be removed after final confirmation.', mbInformation, MB_OK);

  UserWantsKeepLocal := MsgBox(
    'Do you want to keep your logs and updates?'#13#10 +
    'Choose Yes to keep the %LOCALAPPDATA%\Ollama folder, or No to delete it.',
    mbConfirmation, MB_YESNO) = idYes;

  if UserWantsKeepLocal then
    MsgBox('The %LOCALAPPDATA%\Ollama folder (logs, updates) will be kept.', mbInformation, MB_OK)
  else
    MsgBox('The %LOCALAPPDATA%\Ollama folder will be removed after final confirmation.', mbInformation, MB_OK);

  Result := True; // Continue with uninstallation.
end;

{ Conditionally skip the default uninstall confirmation page.
  If the user chose to keep their models (UserWantsKeep=True), skip the confirmation page;
  otherwise, show it. }
function ShouldSkipPage(PageID: Integer): Boolean;
begin
  if PageID = wpUninstallConfirm then
    Result := UserWantsKeep
  else
    Result := False;
end;

{ After final confirmation (during the usUninstall step), delete folders that the user chose to remove. }
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  UserFolder, LocalFolder: string;
begin
  if CurUninstallStep = usUninstall then
  begin
    if not UserWantsKeep then
    begin
      UserFolder := ExpandConstant('{%USERPROFILE}\.ollama');
      if DirExists(UserFolder) then
      begin
        if not DeleteDirectoryRecursive(UserFolder) then
          MsgBox('Failed to delete the .ollama folder completely.', mbError, MB_OK);
      end;
    end;
    if not UserWantsKeepLocal then
    begin
      LocalFolder := ExpandConstant('{%LOCALAPPDATA}\Ollama');
      if DirExists(LocalFolder) then
      begin
        if not DeleteDirectoryRecursive(LocalFolder) then
          MsgBox('Failed to delete the %LOCALAPPDATA%\Ollama folder completely.', mbError, MB_OK);
      end;
    end;
  end;
end;

function NeedsAddPath(Param: string): Boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  { Look for the path with leading and trailing semicolon.
    Pos() returns 0 if not found. }
  Result := Pos(';' + ExpandConstant(Param) + ';', ';' + OrigPath + ';') = 0;
end;

{ --- VC++ Redistributable discovery code --- }
const
  VCRTL_MIN_V1 = 14;
  VCRTL_MIN_V2 = 40;
  VCRTL_MIN_V3 = 33807;
  VCRTL_MIN_V4 = 0;

function vc_redist_needed(): Boolean;
var
  sRegKey: string;
  v1, v2, v3, v4: Cardinal;
begin
  sRegKey := 'SOFTWARE\WOW6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes\arm64';
  if (RegQueryDWordValue(HKEY_LOCAL_MACHINE, sRegKey, 'Major', v1) and
      RegQueryDWordValue(HKEY_LOCAL_MACHINE, sRegKey, 'Minor', v2) and
      RegQueryDWordValue(HKEY_LOCAL_MACHINE, sRegKey, 'Bld', v3) and
      RegQueryDWordValue(HKEY_LOCAL_MACHINE, sRegKey, 'RBld', v4)) then
  begin
    Log('VC Redist version: ' + IntToStr(v1) + '.' + IntToStr(v2) + '.' + IntToStr(v3) + '.' + IntToStr(v4));
    { Return true if the installed version is less than the required version. }
    Result := not ((v1 > VCRTL_MIN_V1) or ((v1 = VCRTL_MIN_V1) and
               ((v2 > VCRTL_MIN_V2) or ((v2 = VCRTL_MIN_V2) and
               ((v3 > VCRTL_MIN_V3) or ((v3 = VCRTL_MIN_V3) and (v4 >= VCRTL_MIN_V4)))))));
  end
  else
    Result := True;
end;
