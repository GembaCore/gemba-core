#!/usr/bin/env pwsh
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$FeatureId = ""
)

$ErrorActionPreference = 'Stop'

function Find-ProjectRoot {
    $current = (Get-Location).Path
    while ($true) {
        if ((Test-Path (Join-Path $current '.specify')) -or (Test-Path (Join-Path $current 'specs'))) {
            return $current
        }
        $parent = Split-Path $current -Parent
        if ($parent -eq $current) { return $null }
        $current = $parent
    }
}

function Read-YamlValue {
    param([string]$Path, [string]$Key)
    if (-not (Test-Path $Path)) { return "" }
    foreach ($line in Get-Content $Path) {
        if ($line -match "^\s*$([regex]::Escape($Key)):\s*(.+)\s*$") {
            return ($matches[1].Trim() -replace '^["'']' -replace '["'']$')
        }
    }
    return ""
}

function Test-Truthy {
    param([string]$Value)
    $v = ($Value ?? '').ToLowerInvariant()
    return @('true', '1', 'yes', 'on') -contains $v
}

function Get-FeatureId {
    param([string]$Explicit)
    if ($Explicit) { return $Explicit }
    if ($env:SPECIFY_FEATURE) { return $env:SPECIFY_FEATURE }
    if (Get-Command git -ErrorAction SilentlyContinue) {
        git rev-parse --is-inside-work-tree 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $branch = (git rev-parse --abbrev-ref HEAD).Trim()
            $branch = ($branch -split '/')[-1]
            if ((Test-Path "specs/$branch") -or (Test-Path ".specify/specs/$branch")) {
                return $branch
            }
        }
    }
    $candidates = @()
    foreach ($dir in @('specs', '.specify/specs')) {
        if (Test-Path $dir) {
            $candidates += Get-ChildItem $dir -Directory
        }
    }
    if ($candidates.Count -gt 0) {
        return ($candidates | Sort-Object LastWriteTime -Descending | Select-Object -First 1).Name
    }
    return ""
}

$root = Find-ProjectRoot
if (-not $root) {
    throw "[gemba] Could not find a Spec Kit project root"
}
Set-Location $root

$config = Join-Path $root '.specify/extensions/gemba/gemba-config.yml'
$apiBase = if ($env:GEMBA_API_BASE) { $env:GEMBA_API_BASE } else { Read-YamlValue $config 'api_base' }
$authToken = if ($env:GEMBA_AUTH_TOKEN) { $env:GEMBA_AUTH_TOKEN } else { Read-YamlValue $config 'auth_token' }
$autoApply = if ($env:GEMBA_SYNC_AUTO_APPLY) { $env:GEMBA_SYNC_AUTO_APPLY } else { Read-YamlValue $config 'auto_apply' }
$allowDeletes = if ($env:GEMBA_SYNC_ALLOW_DELETES) { $env:GEMBA_SYNC_ALLOW_DELETES } else { Read-YamlValue $config 'allow_deletes' }
if (-not $apiBase) { $apiBase = 'http://127.0.0.1:7666/api' }

$feature = Get-FeatureId $FeatureId
if (-not $feature) {
    throw "[gemba] Could not infer Spec Kit feature id"
}

$headers = @{}
if ($authToken) {
    $headers['Authorization'] = "Bearer $authToken"
}

$plan = Invoke-RestMethod -Uri "$apiBase/spec-kit/features/$feature/sync-plan" -Headers $headers
Write-Host "Gemba Spec Kit sync plan for $feature"
Write-Host "create: $($plan.counts.create) update: $($plan.counts.update) delete: $($plan.counts.delete)"
Write-Host "hash: $($plan.hash)"

if (-not (Test-Truthy $autoApply)) {
    Write-Host "Review in Gemba: Refine -> Spec Kit"
    exit 0
}

if (($plan.counts.delete -gt 0) -and -not (Test-Truthy $allowDeletes)) {
    throw "[gemba] Refusing to auto-apply $($plan.counts.delete) delete(s). Set allow_deletes true after reviewing the plan."
}

$headers['Content-Type'] = 'application/json'
$headers['X-GEMBA-Confirm'] = "speckit-gemba-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
$body = @{
    plan_hash = $plan.hash
    allow_deletes = (Test-Truthy $allowDeletes)
} | ConvertTo-Json -Compress

$result = Invoke-RestMethod -Method Post -Uri "$apiBase/spec-kit/features/$feature/sync-to-beads" -Headers $headers -Body $body
Write-Host "Applied Gemba sync for $feature"
Write-Host "created: $(($result.created ?? @()).Count) updated: $(($result.updated ?? @()).Count) deleted: $(($result.deleted ?? @()).Count)"
