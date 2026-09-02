param(
  [Parameter(Mandatory=$true)][ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+(?:-(?:preview|rc)\.[0-9]+)?$')][string]$Version,
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\AgentComms"
)
$ErrorActionPreference = 'Stop'
$repo = 'DhanushSantosh/AgentComms'
$api = "https://api.github.com/repos/$repo/releases/tags/$Version"
$release = Invoke-RestMethod $api
if (-not $release -or $release.draft -or $release.tag_name -ne $Version) { throw "No matching published release is available." }
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "agent-comms-windows-$arch.exe"
$asset = $release.assets | Where-Object name -eq $name
$checks = $release.assets | Where-Object name -eq 'checksums.txt'
$bundle = $release.assets | Where-Object name -eq "$name.bundle"
$verifierName = "agent-comms-verify-windows-$arch.exe"
$verifierAsset = $release.assets | Where-Object name -eq $verifierName
if (-not $asset -or -not $checks -or -not $bundle -or -not $verifierAsset) { throw 'Release is missing a binary, checksum, Cosign bundle, or verifier.' }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("agent-comms-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
  Invoke-WebRequest $asset.browser_download_url -OutFile (Join-Path $tmp $name)
  Invoke-WebRequest $checks.browser_download_url -OutFile (Join-Path $tmp 'checksums.txt')
  Invoke-WebRequest $bundle.browser_download_url -OutFile (Join-Path $tmp "$name.bundle")
  Invoke-WebRequest $verifierAsset.browser_download_url -OutFile (Join-Path $tmp $verifierName)
  $pins = Invoke-RestMethod "https://raw.githubusercontent.com/$repo/$Version/release-verifier-checksums.txt"
  $pinMatches = @($pins -split "`n" | Where-Object { $_ -match "^$([regex]::Escape($Version))\s+$([regex]::Escape($verifierName))\s+([0-9a-f]{64})\s*$" })
  if ($pinMatches.Count -ne 1) { throw "Release tag has no unique verifier pin for $verifierName." }
  $pinned = (($pinMatches[0] -split '\s+')[2]).ToLowerInvariant()
  $verifierActual = (Get-FileHash (Join-Path $tmp $verifierName) -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($verifierActual -ne $pinned) { throw 'Trusted verifier SHA-256 verification failed.' }
  $checksums = Get-Content (Join-Path $tmp 'checksums.txt')
  foreach ($f in @($name)) {
    $expected = (($checksums | Where-Object { $_ -match "\s$f$" }).Split())[0]
    $actual = (Get-FileHash (Join-Path $tmp $f) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "SHA-256 verification failed for $f." }
  }
  $verifier = Join-Path $tmp $verifierName
  $identity = '^' + [regex]::Escape("https://github.com/$repo/.github/workflows/release.yml@refs/tags/$Version") + '$'
  & $verifier --bundle (Join-Path $tmp "$name.bundle") --certificate-identity-regexp $identity --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' (Join-Path $tmp $name)
  if ($LASTEXITCODE -ne 0) { throw 'Release verification failed.' }
  New-Item -ItemType Directory -Force $InstallDir | Out-Null
  $target = Join-Path $InstallDir 'agent-comms.exe'
  if (Test-Path $target) { Copy-Item $target "$target.previous" -Force }
  Move-Item (Join-Path $tmp $name) $target -Force
  $userPath = [Environment]::GetEnvironmentVariable('Path','User')
  if (($userPath -split ';') -notcontains $InstallDir) { [Environment]::SetEnvironmentVariable('Path',(($userPath.TrimEnd(';') + ';' + $InstallDir).TrimStart(';')),'User') }
  Write-Host "Installed Agent Comms $($release.tag_name) to $target"
} finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
