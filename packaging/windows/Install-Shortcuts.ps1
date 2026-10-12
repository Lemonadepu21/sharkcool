$ErrorActionPreference = 'Stop'
$exe = Join-Path $PSScriptRoot 'SharkCool.exe'
if (!(Test-Path $exe)) { throw 'Please extract the entire application first.' }
$shell = New-Object -ComObject WScript.Shell
foreach ($folder in @([Environment]::GetFolderPath('Desktop'),[Environment]::GetFolderPath('Programs'))) {
    if (!$folder) { continue }
    $shortcut = $shell.CreateShortcut((Join-Path $folder 'SharkCool.lnk'))
    $shortcut.TargetPath = $exe
    $shortcut.WorkingDirectory = $PSScriptRoot
    $shortcut.IconLocation = "$exe,0"
    $shortcut.Save()
}
Write-Host 'SharkCool shortcuts installed. Keep the application folder at this location.'
