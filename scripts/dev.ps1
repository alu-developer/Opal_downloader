param(
    [Parameter(Position = 0)]
    [ValidateSet("deps", "fmt", "vet", "test", "build", "all", "playwright")]
    [string]$Task = "all"
)

$ErrorActionPreference = "Stop"
$go = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $go)) {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $cmd) {
        throw "Go executable not found. Install Go or add it to PATH."
    }
    $go = $cmd.Source
}

Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    switch ($Task) {
        "deps" {
            & $go mod tidy
        }
        "fmt" {
            & $go fmt ./...
        }
        "vet" {
            & $go vet ./...
        }
        "test" {
            & $go test ./...
        }
        "build" {
            & $go build ./...
        }
        "playwright" {
            & $go run github.com/playwright-community/playwright-go/cmd/playwright@latest install
        }
        "all" {
            & $go mod tidy
            & $go fmt ./...
            & $go vet ./...
            & $go test ./...
            & $go build ./...
        }
    }
}
finally {
    Pop-Location
}
