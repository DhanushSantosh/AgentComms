param(
  [ValidateSet('stable','preview')][string]$Channel = 'stable',
  [string]$Version = '',
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\AgentComms"
)
$ErrorActionPreference = 'Stop'
$repo = 'DhanushSantosh/AgentComms'
$api = if ($Version) { "https://api.github.com/repos/$repo/releases/tags/$Version" } else { "https://api.github.com/repos/$repo/releases" }
$release = Invoke-RestMethod $api
if (-not $Version) {
  $release = $release | Where-Object { -not $_.draft -and ($Channel -eq 'preview' -or -not $_.prerelease) } | Select-Object -First 1
}
if (-not $release) { throw "No $Channel release is available." }
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "agent-comms-windows-$arch.exe"
$asset = $release.assets | Where-Object name -eq $name
$checks = $release.assets | Where-Object name -eq 'checksums.txt'
$bundle = $release.assets | Where-Object name -eq "$name.bundle"
if (-not $asset -or -not $checks -or -not $bundle) { throw 'Release is missing a binary, checksum, or Cosign bundle.' }
if (-not (Get-Command cosign -ErrorAction SilentlyContinue)) { throw 'cosign is required to verify Agent Comms. Install it from https://docs.sigstore.dev/cosign/system_config/installation/' }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("agent-comms-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
  Invoke-WebRequest $asset.browser_download_url -OutFile (Join-Path $tmp $name)
  Invoke-WebRequest $checks.browser_download_url -OutFile (Join-Path $tmp 'checksums.txt')
  Invoke-WebRequest $bundle.browser_download_url -OutFile (Join-Path $tmp "$name.bundle")
  $expected = ((Get-Content (Join-Path $tmp 'checksums.txt')) | Where-Object { $_ -match "\s$name$" }).Split()[0]
  $actual = (Get-FileHash (Join-Path $tmp $name) -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { throw 'SHA-256 verification failed.' }
  & cosign verify-blob --bundle (Join-Path $tmp "$name.bundle") --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/' --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' (Join-Path $tmp $name)
  if ($LASTEXITCODE -ne 0) { throw 'Cosign verification failed.' }
  New-Item -ItemType Directory -Force $InstallDir | Out-Null
  $target = Join-Path $InstallDir 'agent-comms.exe'
  if (Test-Path $target) { Copy-Item $target "$target.previous" -Force }
  Move-Item (Join-Path $tmp $name) $target -Force
  $userPath = [Environment]::GetEnvironmentVariable('Path','User')
  if (($userPath -split ';') -notcontains $InstallDir) { [Environment]::SetEnvironmentVariable('Path',(($userPath.TrimEnd(';') + ';' + $InstallDir).TrimStart(';')),'User') }
  Write-Host "Installed Agent Comms $($release.tag_name) to $target"
} finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
