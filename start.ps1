$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  ECS Card Game - Quick Start Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

$step = 1

Write-Host "[$step] Checking prerequisites..." -ForegroundColor Yellow
$step++

$dockerInstalled = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerInstalled) {
    Write-Error "Docker is not installed. Please install Docker Desktop first."
    exit 1
}
Write-Host "  ✓ Docker installed" -ForegroundColor Green

$goInstalled = Get-Command go -ErrorAction SilentlyContinue
if (-not $goInstalled) {
    Write-Warning "Go is not installed. You won't be able to build locally."
} else {
    Write-Host "  ✓ Go installed" -ForegroundColor Green
}

$protocInstalled = Get-Command protoc -ErrorAction SilentlyContinue
if (-not $protocInstalled) {
    Write-Warning "protoc is not installed. You won't be able to generate gRPC code."
} else {
    Write-Host "  ✓ protoc installed" -ForegroundColor Green
}

Write-Host ""
Write-Host "[$step] Creating .env file from .env.example..." -ForegroundColor Yellow
$step++
if (-not (Test-Path ".env")) {
    Copy-Item ".env.example" ".env"
    Write-Host "  ✓ .env file created" -ForegroundColor Green
} else {
    Write-Host "  ✓ .env file already exists" -ForegroundColor Green
}

Write-Host ""
Write-Host "[$step] Starting Docker containers..." -ForegroundColor Yellow
$step++

Write-Host "  Building images..."
docker-compose build
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to build Docker images"
    exit 1
}

Write-Host "  Starting services..."
docker-compose up -d
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to start Docker containers"
    exit 1
}

Write-Host ""
Write-Host "[$step] Waiting for services to be ready..." -ForegroundColor Yellow
$step++

$maxRetries = 30
$retryCount = 0
$ready = $false

while ($retryCount -lt $maxRetries -and -not $ready) {
    try {
        $redisReady = docker exec redis redis-cli ping 2>$null
        $mongoReady = docker exec mongodb mongosh --eval "db.adminCommand('ping')" 2>$null
        
        if ($redisReady -eq "PONG" -and $mongoReady -match "ok") {
            $ready = $true
            Write-Host "  ✓ All services ready" -ForegroundColor Green
        }
    } catch {
        $retryCount++
        Write-Host "  Waiting... ($retryCount/$maxRetries)"
        Start-Sleep -Seconds 2
    }
}

if (-not $ready) {
    Write-Warning "Services may not be fully ready. Check logs for details."
}

Write-Host ""
Write-Host "[$step] Service Information" -ForegroundColor Yellow
$step++

Write-Host ""
Write-Host "  Backend Services:" -ForegroundColor Cyan
Write-Host "    • Redis: localhost:6379"
Write-Host "    • MongoDB: localhost:27017"
Write-Host "    • Game Server: localhost:50051 (gRPC)"
Write-Host "    • Match Server: localhost:50052 (gRPC)"
Write-Host "    • Envoy Gateway: localhost:8080 (gRPC-Web)"
Write-Host "    • Envoy Admin: localhost:9901"
Write-Host ""
Write-Host "  Docker Commands:" -ForegroundColor Cyan
Write-Host "    • View logs: docker-compose logs -f"
Write-Host "    • Stop services: docker-compose down"
Write-Host "    • Restart: docker-compose restart"
Write-Host ""
Write-Host "  Unity Setup:" -ForegroundColor Cyan
Write-Host "    1. Open Unity Hub and add project: frontend/UnityCardGame"
Write-Host "    2. Wait for packages to import"
Write-Host "    3. Generate gRPC code: cd frontend/UnityCardGame ; ./generate_grpc_unity.ps1"
Write-Host "    4. Open scene: Assets/Scenes/Main.unity"
Write-Host "    5. Press Play to start"
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  All services started successfully!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
