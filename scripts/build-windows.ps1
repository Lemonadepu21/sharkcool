param([Parameter(Mandatory=$true)][string]$QtRoot)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $QtRoot = (Resolve-Path $QtRoot).Path
    $env:PATH = "$QtRoot\bin;$env:PATH"
    $env:GOFLAGS = '-mod=vendor'
    $env:CGO_ENABLED = '0'
    $version = [regex]::Match((Get-Content internal/buildinfo/version.go -Raw), 'Release = "([^"]+)"').Groups[1].Value
    if (!$version) { throw 'Missing release version' }
    $build = Join-Path $root 'dist/windows-build'
    & cmake -S desktop -B $build -G 'Visual Studio 17 2022' -A x64 "-DCMAKE_PREFIX_PATH=$QtRoot" -DBUILD_TESTING=ON -DBUILD_TRANSPORT_BRIDGE=ON
    if ($LASTEXITCODE) { throw 'CMake configuration failed' }
    & cmake --build $build --config Release --parallel 2
    if ($LASTEXITCODE) { throw 'Native Windows build failed' }
    & go test ./...
    if ($LASTEXITCODE) { throw 'Go tests failed' }
    & go vet ./...
    if ($LASTEXITCODE) { throw 'Go vet failed' }
    & ctest --test-dir $build -C Release --output-on-failure
    if ($LASTEXITCODE) { throw 'Native Windows tests failed' }
    & node internal/webui/tests/app.test.cjs
    if ($LASTEXITCODE) { throw 'Web UI tests failed' }
    $name = "SharkCool-$version-windows-x64"
    $package = Join-Path $root "dist/$name"
    if (Test-Path $package) { Remove-Item -Recurse -Force $package }
    New-Item -ItemType Directory -Path $package | Out-Null
    & go build -buildvcs=false -trimpath -ldflags '-s -w -H windowsgui' -o "$package/sharkcool-service.exe" ./cmd/sharkcool
    if ($LASTEXITCODE) { throw 'Windows service build failed' }
    foreach ($file in @('SharkCool.exe', 'sharkcool-window.exe', 'sharkcool-bridge.exe')) {
        Copy-Item "$build/Release/$file" $package
    }
    foreach ($file in @('sharkcool-window.exe', 'sharkcool-bridge.exe')) {
        & "$QtRoot/bin/windeployqt.exe" --release --no-translations --no-opengl-sw --no-system-d3d-compiler --compiler-runtime "$package/$file"
        if ($LASTEXITCODE) { throw "Deployment failed for $file" }
    }
    # Deploy the MSVC CRT app-locally so first launch needs no separate runtime
    # installation or administrator access. Windows 10 already supplies UCRT.
    $vswhere = "${env:ProgramFiles(x86)}/Microsoft Visual Studio/Installer/vswhere.exe"
    $vs = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if (!$vs) { throw 'Visual Studio C++ runtime directory not found' }
    $runtime = Get-ChildItem "$vs/VC/Redist/MSVC" -Directory | Sort-Object { [version]$_.Name } -Descending | Select-Object -First 1
    $crt = Join-Path $runtime.FullName 'x64/Microsoft.VC143.CRT'
    if (!(Test-Path "$crt/vcruntime140.dll")) { throw 'Redistributable MSVC CRT DLLs not found' }
    Copy-Item "$crt/*.dll" $package
    Get-ChildItem $package -Filter 'vc_redist*.exe' | Remove-Item
    Copy-Item README.md,PROTOCOL.md,LICENSE $package
    Copy-Item packaging/windows/README-Windows.md "$package/README-Windows.md"
    Copy-Item packaging/windows/Install-Shortcuts.ps1,packaging/windows/Install-Shortcuts.cmd,packaging/windows/Stop-SharkCool.ps1,packaging/windows/Stop-SharkCool.cmd $package
    Copy-Item packaging/sharkcool.ico "$package/SharkCool.ico"
    New-Item -ItemType Directory "$package/licenses" | Out-Null
    Copy-Item packaging/licenses/* "$package/licenses/"
    Copy-Item packaging/ASSET-NOTICE.md "$package/licenses/ASSET-NOTICE.md"
    Copy-Item packaging/windows/THIRD-PARTY.md "$package/licenses/THIRD-PARTY.md"
    if (Test-Path "$QtRoot/LICENSES") { Copy-Item -Recurse "$QtRoot/LICENSES" "$package/licenses/Qt-LICENSES" }
    # A portable package must launch without the build-machine Qt directory.
    $savedPath = $env:PATH
    try {
        $env:PATH = ($savedPath.Split(';') | Where-Object { $_ -notlike "$QtRoot*" }) -join ';'
        & "$package/sharkcool-window.exe" --version
        if ($LASTEXITCODE) { throw 'Packaged window runtime check failed' }
        & "$package/sharkcool-bridge.exe" --self-test
        if ($LASTEXITCODE) { throw 'Packaged bridge runtime check failed' }
        & python scripts/windows-smoke.py $package
        if ($LASTEXITCODE) { throw 'Packaged application smoke test failed' }
    } finally { $env:PATH = $savedPath }
    Compress-Archive -Path $package -DestinationPath "dist/$name.zip" -Force
    $hash = (Get-FileHash "dist/$name.zip" -Algorithm SHA256).Hash.ToLower()
    "$hash  $name.zip" | Set-Content "dist/$name.zip.sha256" -Encoding ascii
    $iscc = "${env:ProgramFiles(x86)}/Inno Setup 6/ISCC.exe"
    if (!(Test-Path $iscc)) { throw 'Inno Setup 6 is needed to build the downloadable installer EXE' }
    & $iscc "/DAppVersion=$version" "/DSourceDir=$package" "/DOutputDir=$root/dist" packaging/windows/installer.iss
    if ($LASTEXITCODE) { throw 'Installer EXE build failed' }
    $setupName = "SharkCool-$version-windows-x64-setup.exe"
    $setupHash = (Get-FileHash "dist/$setupName" -Algorithm SHA256).Hash.ToLower()
    "$setupHash  $setupName" | Set-Content "dist/$setupName.sha256" -Encoding ascii
    & python scripts/windows-installer-smoke.py "$root/dist/$setupName"
    if ($LASTEXITCODE) { throw 'Installer EXE smoke test failed' }
    Write-Output "Portable Windows package: dist/$name.zip"
} finally { Pop-Location }
