param([switch]$Uninstall, [switch]$SkipDriver, [string]$Source = (Join-Path $PSScriptRoot 'sensors'))
$ErrorActionPreference = 'Stop'
$name = 'SharkCoolSensors'
$target = Join-Path $env:ProgramFiles $name
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (!(New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $parameters = @('-NoProfile','-ExecutionPolicy','Bypass','-File',('"'+$PSCommandPath+'"'),'-Source',('"'+$Source+'"'))
    if ($Uninstall) { $parameters += '-Uninstall' }
    if ($SkipDriver) { $parameters += '-SkipDriver' }
    $elevated = Start-Process powershell.exe -Verb RunAs -ArgumentList $parameters -Wait -PassThru
    exit $elevated.ExitCode
}
$existing = Get-Service $name -ErrorAction SilentlyContinue
if ($existing) {
    Stop-Service $name -Force
    $existing.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(15))
}
if ($Uninstall) {
    if ($existing) {
        & sc.exe delete $name | Out-Null
        if ($LASTEXITCODE) { throw 'Could not remove CPU temperature service' }
    }
    if (Test-Path $target) { Remove-Item -Recurse -Force $target }
    # PawnIO may also be used by other monitoring applications.
    return
}
$Source = (Resolve-Path $Source).Path
if (!(Test-Path (Join-Path $Source 'sharkcool-sensors.exe'))) { throw 'CPU temperature component is missing' }
if (!$SkipDriver) {
    $driver = Join-Path $Source 'PawnIO_setup.exe'
    if (!(Test-Path $driver)) { throw 'PawnIO installer is missing' }
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
}
# The privileged executable always lives under protected Program Files.
if ($Source -ne $target) {
    New-Item -ItemType Directory -Force $target | Out-Null
    Copy-Item (Join-Path $Source '*') $target -Force -Recurse
}
$binary = '"' + (Join-Path $target 'sharkcool-sensors.exe') + '"'
if ($existing) {
    & sc.exe config $name binPath= $binary start= auto obj= LocalSystem | Out-Null
} else {
    & sc.exe create $name binPath= $binary start= auto obj= LocalSystem DisplayName= 'SharkCool CPU Temperature' | Out-Null
}
if ($LASTEXITCODE) { throw 'Could not register CPU temperature service' }
& sc.exe failure $name reset= 86400 actions= restart/5000/restart/15000/restart/60000 | Out-Null
if ($LASTEXITCODE) { throw 'Could not configure CPU temperature service recovery' }
Start-Service $name
(Get-Service $name).WaitForStatus('Running', [TimeSpan]::FromSeconds(15))
Write-Host 'SharkCool CPU temperature service is ready.'
