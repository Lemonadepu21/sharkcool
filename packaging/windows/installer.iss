#ifndef AppVersion
#define AppVersion "1.13.0"
#endif
#ifndef SourceDir
#error SourceDir must point to the complete portable application directory
#endif
#ifndef OutputDir
#define OutputDir "."
#endif
[Setup]
AppId={{ECED5B67-A396-4B5E-89AB-4CBB620A5206}
AppName=SharkCool
AppVersion={#AppVersion}
AppPublisher=SharkCool contributors
DefaultDirName={autopf}\SharkCool
UsePreviousAppDir=no
DefaultGroupName=SharkCool
PrivilegesRequired=admin
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
MinVersion=10.0.17763
OutputDir={#OutputDir}
OutputBaseFilename=SharkCool-{#AppVersion}-windows-x64-setup
SetupIconFile={#SourceDir}\SharkCool.ico
UninstallDisplayIcon={app}\SharkCool.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
DisableProgramGroupPage=yes
AppMutex=Local\SharkCool-BRB02-device
CloseApplications=no
RestartApplications=no
LicenseFile={#SourceDir}\LICENSE
[Tasks]
Name: desktopicon; Description: "Create a desktop shortcut"; Flags: checkedonce
Name: cputemp; Description: "Enable CPU temperature monitoring"; Flags: checkedonce
[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Excludes: "sensors\*,Enable-CPU-Temperature.cmd"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\sensors\*"; DestDir: "{commonpf}\SharkCoolSensors"; Flags: ignoreversion recursesubdirs createallsubdirs; Tasks: cputemp
[Icons]
Name: "{autoprograms}\SharkCool"; Filename: "{app}\SharkCool.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\SharkCool"; Filename: "{app}\SharkCool.exe"; WorkingDir: "{app}"; Tasks: desktopicon
[InstallDelete]
Type: files; Name: "{userdesktop}\SharkCool.lnk"
Type: files; Name: "{userprograms}\SharkCool.lnk"
[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\Install-Shortcuts.ps1"" -UpdateAutostartOnly"; Flags: runhidden waituntilterminated runasoriginaluser
Filename: "{app}\SharkCool.exe"; Description: "Launch SharkCool"; Flags: nowait postinstall skipifsilent
[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\Stop-SharkCool.ps1"""; Flags: runhidden waituntilterminated
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{commonpf}\SharkCoolSensors\Install-Sensors.ps1"" -Uninstall"; Flags: runhidden waituntilterminated skipifdoesntexist
[UninstallDelete]
Type: files; Name: "{userstartup}\SharkCool.lnk"

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var ResultCode: Integer;
begin
  Result := '';
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoProfile -NonInteractive -Command "$ErrorActionPreference=''Stop''; $env:PSModulePath=$PSHOME+''\Modules;''+$env:PSModulePath; $s=Get-Service SharkCoolSensors -ErrorAction SilentlyContinue; if($s){Stop-Service SharkCoolSensors -Force; $s.WaitForStatus(''Stopped'',[TimeSpan]::FromSeconds(15))}; exit 0"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    Result := 'Please stop the SharkCool CPU temperature service before updating.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
var ResultCode: Integer;
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('cputemp') then
    if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
      ExpandConstant('-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "{commonpf}\SharkCoolSensors\Install-Sensors.ps1" -Source "{commonpf}\SharkCoolSensors"'),
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
      RaiseException('CPU temperature service installation failed. Please rerun Setup.');
end;
