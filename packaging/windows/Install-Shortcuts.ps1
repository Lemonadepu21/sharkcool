param([switch]$UpdateAutostartOnly)
$ErrorActionPreference = 'Stop'
$exe = Join-Path $PSScriptRoot 'SharkCool.exe'
if (!(Test-Path $exe)) { throw 'Please extract the entire application first.' }
$shell = New-Object -ComObject WScript.Shell
if (!$UpdateAutostartOnly) { foreach ($folder in @([Environment]::GetFolderPath('Desktop'),[Environment]::GetFolderPath('Programs'))) {
    if (!$folder) { continue }
    $shortcut = $shell.CreateShortcut((Join-Path $folder 'SharkCool.lnk'))
    $shortcut.TargetPath = $exe
    $shortcut.WorkingDirectory = $PSScriptRoot
    $shortcut.IconLocation = "$exe,0"
    $shortcut.Save()
}
}
$startup = Join-Path ([Environment]::GetFolderPath('Startup')) 'SharkCool.lnk'
if (Test-Path $startup) {
    $shortcut = $shell.CreateShortcut($startup)
    $shortcut.TargetPath = $exe
    $shortcut.Arguments = 'gui --tray'
    $shortcut.WorkingDirectory = $PSScriptRoot
    $shortcut.IconLocation = "$exe,0"
    $shortcut.Save()
}
if (!$UpdateAutostartOnly) { Write-Host 'SharkCool shortcuts installed. Keep the application folder at this location.' }
