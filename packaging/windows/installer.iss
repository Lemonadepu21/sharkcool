#ifndef AppVersion
#define AppVersion "1.12.1"
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
DefaultDirName={localappdata}\Programs\SharkCool
DefaultGroupName=SharkCool
PrivilegesRequired=lowest
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
CloseApplications=yes
RestartApplications=no
LicenseFile={#SourceDir}\LICENSE
[Tasks]
Name: desktopicon; Description: "Create a desktop shortcut"; Flags: checkedonce
[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
[Icons]
Name: "{autoprograms}\SharkCool"; Filename: "{app}\SharkCool.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\SharkCool"; Filename: "{app}\SharkCool.exe"; WorkingDir: "{app}"; Tasks: desktopicon
[Run]
Filename: "{app}\SharkCool.exe"; Description: "Launch SharkCool"; Flags: nowait postinstall skipifsilent
[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\Stop-SharkCool.ps1"""; Flags: runhidden waituntilterminated
[UninstallDelete]
Type: files; Name: "{userstartup}\SharkCool.lnk"
