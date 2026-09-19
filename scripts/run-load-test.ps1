# Roda a carga formal (k6) e anexa atraso da outbox a partir de GET /metrics.
param(
  [string]$BaseUrl = "http://localhost:8090",
  [string]$KeycloakUrl = "http://localhost:8088"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
  [System.Environment]::GetEnvironmentVariable("Path", "User")

New-Item -ItemType Directory -Force -Path "loadtests\results" | Out-Null

Write-Host "== k6 wagering-load =="
k6 run `
  --summary-trend-stats="avg,min,med,p(90),p(95),p(99),max" `
  -e "BASE_URL=$BaseUrl" `
  -e "KEYCLOAK_URL=$KeycloakUrl" `
  loadtests/wagering-load.js

Write-Host "== scrape outbox lag =="
try {
  $metrics = curl.exe -sf "$BaseUrl/metrics"
} catch {
  Write-Warning "nao foi possivel ler $BaseUrl/metrics"
  exit 0
}

$sum = ($metrics | Select-String -Pattern '^wagering_outbox_lag_seconds_sum\s+(\S+)').Matches |
  ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
$count = ($metrics | Select-String -Pattern '^wagering_outbox_lag_seconds_count\s+(\S+)').Matches |
  ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1

$conflictTotal = 0.0
foreach ($m in ($metrics | Select-String -Pattern '^wagering_concurrency_conflicts_total(?:\{[^}]*\})?\s+(\S+)')) {
  $conflictTotal += [double]$m.Matches[0].Groups[1].Value
}

$avg = "n/a"
if ($count -and [double]$count -gt 0 -and $sum) {
  $avg = ([double]$sum / [double]$count).ToString("0.######", [cultureinfo]::InvariantCulture)
}

$p95 = "n/a"
if ($count -and [double]$count -gt 0) {
  $target = [double]$count * 0.95
  foreach ($line in ($metrics -split "`n")) {
    if ($line -match 'wagering_outbox_lag_seconds_bucket\{le="([^"]+)"\}\s+(\S+)') {
      $le = $Matches[1]
      if ($le -eq "+Inf") { continue }
      if ([double]$Matches[2] -ge $target) {
        $p95 = $le
        break
      }
    }
  }
}

$block = @"
- scrape: ``$BaseUrl/metrics``
- amostras (``_count``): $($count|ForEach-Object { $_ })
- avg lag (s): $avg
- p95 approx (s, bucket ``le``): $p95
- Prometheus ``wagering_concurrency_conflicts_total``: $conflictTotal
"@

$summaryMd = "loadtests\results\latest-summary.md"
if (Test-Path $summaryMd) {
  $content = Get-Content $summaryMd -Raw
  $content = $content -replace "<!-- OUTBOX_LAG -->", "<!-- OUTBOX_LAG -->`r`n$block"
  Set-Content -Path $summaryMd -Value $content -NoNewline
}

$summaryJson = "loadtests\results\latest-summary.json"
if (Test-Path $summaryJson) {
  $json = Get-Content $summaryJson -Raw | ConvertFrom-Json
  $json.outbox_lag = [pscustomobject]@{
    samples            = if ($count) { [double]$count } else { 0 }
    avg_seconds        = if ($avg -ne "n/a") { [double]$avg } else { $null }
    p95_approx_seconds = if ($p95 -ne "n/a") { [double]$p95 } else { $null }
    source             = "GET /metrics → wagering_outbox_lag_seconds"
  }
  if (-not $json.outcomes) { $json | Add-Member -NotePropertyName outcomes -NotePropertyValue (@{}) }
  $json.outcomes | Add-Member -NotePropertyName prometheus_concurrency_conflicts_total -NotePropertyValue $conflictTotal -Force
  $json | ConvertTo-Json -Depth 6 | Set-Content $summaryJson
}

Write-Host "Relatorio: loadtests/results/latest-summary.md"
