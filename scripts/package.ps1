param([string]$Tag = 'v1.1.0')
$ErrorActionPreference = 'Stop'
if ($Tag -notmatch '^v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$') { throw 'Use a semantic version tag.' }
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Executable = Join-Path $ProjectRoot 'build/bin/ConfigDoctor.exe'
if (!(Test-Path -LiteralPath $Executable)) { throw 'Build ConfigDoctor.exe first.' }
$OutputRoot = Join-Path $ProjectRoot 'artifacts'
New-Item -ItemType Directory -Path $OutputRoot -Force | Out-Null
$Stage = Join-Path $OutputRoot ('portable-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Stage | Out-Null
Copy-Item -LiteralPath $Executable -Destination (Join-Path $Stage 'ConfigDoctor.exe')
foreach ($Name in @('LICENSE','RELEASE_NOTES.md','README.md','SECURITY.md','SUPPORT.md','THIRD_PARTY_NOTICES.md')) { Copy-Item -LiteralPath (Join-Path $ProjectRoot $Name) -Destination $Stage }
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'samples') -Destination $Stage -Recurse
if (Test-Path -LiteralPath (Join-Path $ProjectRoot 'licenses')) { Copy-Item -LiteralPath (Join-Path $ProjectRoot 'licenses') -Destination $Stage -Recurse }
New-Item -ItemType Directory -Path (Join-Path $Stage 'docs/screenshots') -Force | Out-Null
if (Test-Path -LiteralPath (Join-Path $ProjectRoot 'docs/screenshots/inspect.png')) { Copy-Item -LiteralPath (Join-Path $ProjectRoot 'docs/screenshots/inspect.png') -Destination (Join-Path $Stage 'docs/screenshots') }
$Zip = Join-Path $OutputRoot "ConfigDoctor-$Tag-win-x64.zip"
if (Test-Path -LiteralPath $Zip) { throw 'Package already exists; choose a new output directory or remove the previous local candidate explicitly.' }
Compress-Archive -Path (Join-Path $Stage '*') -DestinationPath $Zip -CompressionLevel Optimal
$Hash = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
"$Hash  $([System.IO.Path]::GetFileName($Zip))" | Set-Content -LiteralPath ($Zip + '.sha256') -Encoding ascii
if ((Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant() -ne $Hash) { throw 'Checksum verification failed.' }
Write-Output $Zip
Write-Output "SHA256: $Hash"
