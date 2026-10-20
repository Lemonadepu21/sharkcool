param([switch]$Uninstall, [string]$Source = (Join-Path $PSScriptRoot 'sensors'))
$ErrorActionPreference = 'Stop'
# Prefer this PowerShell host's built-in modules when launched from pwsh.
$env:PSModulePath = (Join-Path $PSHOME 'Modules') + [IO.Path]::PathSeparator + $env:PSModulePath
$name = 'SharkCoolSensors'
$target = Join-Path $env:ProgramFiles $name
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (!(New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $parameters = @('-NoProfile','-ExecutionPolicy','Bypass','-File',('"'+$PSCommandPath+'"'),'-Source',('"'+$Source+'"'))
    if ($Uninstall) { $parameters += '-Uninstall' }
    $elevated = Start-Process powershell.exe -Verb RunAs -ArgumentList $parameters -Wait -PassThru
    exit $elevated.ExitCode
}
$existing = Get-Service $name -ErrorAction SilentlyContinue
$binary = '"' + (Join-Path $target 'sharkcool-sensors.exe') + '"'
if ($existing) {
    $service = Get-CimInstance Win32_Service -Filter "Name='$name'"
    if ($service.PathName -ne $binary -or $service.StartName -ne 'LocalSystem') { throw 'Existing sensor service has an unexpected executable or account' }
}
if ($Uninstall) {
    if ($existing) {
        Stop-Service $name -Force
        $existing.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(15))
        & sc.exe delete $name | Out-Null
        if ($LASTEXITCODE) { throw 'Could not remove temperature service' }
    }
    if (Test-Path $target) { Remove-Item -Recurse -Force $target }
    # PawnIO may also be used by other monitoring applications.
    return
}
$Source = (Resolve-Path $Source).Path
if (!(Test-Path (Join-Path $Source 'sharkcool-sensors.exe'))) { throw 'CPU/GPU temperature component is missing' }
$driver = Join-Path $Source 'PawnIO_setup.exe'
if (!(Test-Path $driver)) { throw 'PawnIO installer is missing' }
if ((Get-FileHash $driver -Algorithm SHA256).Hash.ToLower() -ne 'a3a46226c5e2824f4cdd42be0eecbabfc672c86f7889710f5ab1e6ad385b47a0') { throw 'PawnIO installer checksum is invalid' }
if ((Get-AuthenticodeSignature $driver).Status -ne 'Valid') { throw 'PawnIO installer signature is invalid' }
$driverReady = $false
foreach ($key in @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PawnIO','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\PawnIO')) {
    $entry = Get-ItemProperty $key -ErrorAction SilentlyContinue
    if ($entry) {
        if ([version]$entry.DisplayVersion -lt [version]'2.0') { throw 'Please update the existing PawnIO installation to version 2.0 or newer.' }
        $driverReady = $true
        break
    }
}
if (!$driverReady) {
    $install = Start-Process $driver -ArgumentList '-install','-silent' -Wait -PassThru
    if ($install.ExitCode -ne 0) { throw "PawnIO installation failed: $($install.ExitCode)" }
}
if ($existing) {
    Stop-Service $name -Force
    $existing.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(15))
}
# The privileged executable always lives under protected Program Files.
if ($Source -ne $target) {
    New-Item -ItemType Directory -Force $target | Out-Null
    Copy-Item (Join-Path $Source '*') $target -Force -Recurse
}
if ($existing) {
    Set-Service $name -StartupType Automatic
} else {
    New-Service -Name $name -BinaryPathName $binary -StartupType Automatic -DisplayName 'SharkCool Hardware Sensors' | Out-Null
}
& sc.exe failure $name reset= 86400 actions= restart/5000/restart/15000/restart/60000 | Out-Null
if ($LASTEXITCODE) { throw 'Could not configure CPU/GPU temperature service recovery' }
Start-Service $name
(Get-Service $name).WaitForStatus('Running', [TimeSpan]::FromSeconds(15))
Write-Host 'SharkCool CPU/GPU temperature service is ready.'
