<#
    Baut das MSI-Paket des itop-agent.

    Muss auf Windows laufen: WiX unterstuetzt keine andere Plattform. WiX 5
    laesst sich zwar unter Linux installieren, warnt aber selbst
    ("All behavior after this point is undefined") und scheitert an der
    Pfadpruefung. Deshalb WiX 3.14 als Zip ohne Installation.

    Aufruf:
        .\build-msi.ps1 -Version 0.7.0 -BinPath ..\..\dist\itop-agent.exe
#>
param(
    [string]$Version = "0.7.0",
    [string]$BinPath = "dist\itop-agent.exe",
    [string]$OutDir  = "dist",
    [string]$WixDir  = "C:\wix3"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path "$WixDir\candle.exe")) {
    Write-Host "WiX 3.14 wird nach $WixDir geholt ..."
    New-Item -ItemType Directory -Force -Path $WixDir | Out-Null
    $url = "https://github.com/wixtoolset/wix3/releases/download/wix3141rtm/wix314-binaries.zip"
    Invoke-WebRequest -Uri $url -OutFile "$env:TEMP\wix.zip" -UseBasicParsing
    Expand-Archive -Path "$env:TEMP\wix.zip" -DestinationPath $WixDir -Force
    Remove-Item "$env:TEMP\wix.zip"
}

if (-not (Test-Path $BinPath)) {
    throw "Binary nicht gefunden: $BinPath (zuerst 'make build-windows' ausfuehren)"
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$wxs   = Join-Path $PSScriptRoot "itop-agent.wxs"
$obj   = Join-Path $OutDir "itop-agent.wixobj"
$msi   = Join-Path $OutDir "itop-agent-$Version.msi"

& "$WixDir\candle.exe" -nologo -arch x64 -ext WixUtilExtension `
    "-dVersion=$Version" "-dBinPath=$BinPath" -out $obj $wxs
if ($LASTEXITCODE -ne 0) { throw "candle fehlgeschlagen" }

# ICE-Pruefungen bleiben an: sie finden genau die Fehler, die sonst erst beim
# Ausrollen auffallen (doppelte Komponenten-GUIDs, fehlende Schluesselpfade).
& "$WixDir\light.exe" -nologo -ext WixUtilExtension -out $msi $obj
if ($LASTEXITCODE -ne 0) { throw "light fehlgeschlagen" }

Remove-Item $obj -ErrorAction SilentlyContinue
Remove-Item ($msi -replace '\.msi$', '.wixpdb') -ErrorAction SilentlyContinue
Write-Host "MSI erstellt: $msi ($([math]::Round((Get-Item $msi).Length/1MB,1)) MB)"
