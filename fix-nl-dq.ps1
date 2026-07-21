param(
    [Parameter(Mandatory)]
    [string]$Project,
    [string]$Binary = "D:\Work\Projects\AgentComms\agent-comms.exe"
)

$ErrorActionPreference = "Stop"
$tmpDir = Join-Path $env:TEMP "agent-comms-body-fix"
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

# Get document IDs from list
$listJson = & $Binary document list --project $Project --json 2>&1
$parsed = $listJson | ConvertFrom-Json
if (-not $parsed.ok) { throw "document list failed: $($parsed.error.message)" }
$ids = @($parsed.result.PSObject.Properties.Name)

$fixed = 0
$skipped = 0

foreach ($id in $ids) {
    $raw = & $Binary document show --project $Project $id --json 2>&1
    $doc = $raw | ConvertFrom-Json
    if (-not $doc.ok) { Write-Warning "show failed for $id"; continue }
    $body = $doc.result.body

    if ($body -notmatch '\[NL\]' -and $body -notmatch '\[DQ\]') {
        $skipped++
        continue
    }

    $clean = $body -replace '\[NL\]', "`n" -replace '\[DQ\]', '"'
    $tmpFile = Join-Path $tmpDir "$id.txt"
    [System.IO.File]::WriteAllText($tmpFile, $clean, [System.Text.UTF8Encoding]::new($false))

    Write-Host "Fixing $id..."
    $output = & $Binary document update --project $Project --id $id --title $doc.result.title --body-file $tmpFile 2>&1
    if ($LASTEXITCODE -ne 0) { Write-Warning "  update failed for ${id}: ${output}"; continue }
    $fixed++
}

Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
Write-Host "`nDone: $fixed documents fixed, $skipped already clean."
