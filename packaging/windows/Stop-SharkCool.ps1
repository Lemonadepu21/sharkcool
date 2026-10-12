$ErrorActionPreference = 'Stop'
$folder = [IO.Path]::GetFullPath($PSScriptRoot)
foreach ($name in @('sharkcool-window.exe','sharkcool-service.exe','sharkcool-bridge.exe')) {
    $exe = Join-Path $folder $name
    Get-CimInstance Win32_Process -Filter "Name='$name'" | Where-Object { $_.ExecutablePath -and [IO.Path]::GetFullPath($_.ExecutablePath) -eq $exe } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -ErrorAction SilentlyContinue
    }
}
Write-Host 'SharkCool stopped. Device settings and pairing are preserved.'
