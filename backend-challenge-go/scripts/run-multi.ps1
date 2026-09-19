# Sobe três instâncias independentes da API contra o mesmo Compose de
# dependências (ADR-020). Cada processo tem seu próprio pool e seus workers.
#
#   pwsh ./scripts/run-multi.ps1
#   pwsh ./scripts/run-multi.ps1 -Stop

param(
    [int[]]$Ports = @(8081, 8082, 8083),
    [switch]$Stop
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$pidFile = Join-Path $root ".instances.pid"

if ($Stop) {
    if (-not (Test-Path $pidFile)) {
        Write-Host "Nenhuma instância registrada em $pidFile"
        exit 0
    }
    Get-Content $pidFile | ForEach-Object {
        $processId = [int]$_
        Write-Host "Encerrando instância $processId"
        Stop-Process -Id $processId -ErrorAction SilentlyContinue
    }
    Remove-Item $pidFile
    exit 0
}

Push-Location $root
try {
    go build -o bin/api.exe ./cmd/api
    if ($LASTEXITCODE -ne 0) { throw "falha ao compilar a API" }

    $processIds = foreach ($port in $Ports) {
        $process = Start-Process -FilePath "./bin/api.exe" -PassThru -NoNewWindow `
            -Environment @{ HTTP_PORT = "$port" }
        Write-Host "instância iniciada na porta $port (pid $($process.Id))"
        $process.Id
    }
    $processIds | Set-Content $pidFile
}
finally {
    Pop-Location
}
