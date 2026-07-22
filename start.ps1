# Стартовый скрипт tunnelctl для Windows.
# Проверяет Go, при необходимости предлагает установку, собирает tunnelctl и запускает мастер настройки.
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$DistDir = Join-Path $RootDir "dist"
$Bin = Join-Path $DistDir "tunnelctl.exe"

function Ask-YesNo($Prompt, $DefaultYes = $true) {
    if ($DefaultYes) { $suffix = "[Y/n]" } else { $suffix = "[y/N]" }
    $answer = Read-Host "$Prompt $suffix"
    if ([string]::IsNullOrWhiteSpace($answer)) { return $DefaultYes }
    return $answer -match '^(y|yes|д|да)$'
}

function Has-Command($Name) {
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Install-Go-IfNeeded {
    if (Has-Command "go") {
        Write-Host "Go найден: $(go version)"
        return
    }

    Write-Host "Go не найден. Он нужен для сборки tunnelctl."
    if (Has-Command "winget") {
        Write-Host "Команда установки через winget:"
        Write-Host "  winget install --id GoLang.Go -e"
        if (Ask-YesNo "Выполнить установку Go через winget?" $true) {
            winget install --id GoLang.Go -e
            $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
            return
        }
    }

    if (Has-Command "choco") {
        Write-Host "Команда установки через Chocolatey:"
        Write-Host "  choco install golang -y"
        if (Ask-YesNo "Выполнить установку Go через Chocolatey?" $true) {
            choco install golang -y
            $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
            return
        }
    }

    Write-Host "Автоматическая установка недоступна."
    Write-Host "Установи Go вручную командой winget или choco, затем снова запусти start.cmd:"
    Write-Host "  winget install --id GoLang.Go -e"
    Write-Host "или:"
    Write-Host "  choco install golang -y"
    exit 1
}

function Build-App {
    New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
    Write-Host "Собираю tunnelctl..."
    Write-Host "Команда сборки:"
    Write-Host "  go build -o `"$Bin`" ./cmd/tunnelctl"
    Push-Location $RootDir
    try {
        go mod download
        go build -o $Bin ./cmd/tunnelctl
    } finally {
        Pop-Location
    }
    Write-Host "Готово: $Bin"
}

Write-Host "Мастер первого запуска tunnelctl"
Write-Host "Папка проекта: $RootDir"
Install-Go-IfNeeded
Build-App
Write-Host "Запускаю мастер настройки..."
& $Bin bootstrap
