$ErrorActionPreference = "Stop"

Write-Host "Generating gRPC code for Go backend..."

$protoPath = "./proto"
$goOutPath = "./internal/proto"

if (-not (Test-Path $goOutPath)) {
    New-Item -ItemType Directory -Path $goOutPath -Force | Out-Null
}

$protoFiles = @(
    "game.proto",
    "matchmaking.proto"
)

foreach ($protoFile in $protoFiles) {
    $fullPath = Join-Path $protoPath $protoFile
    if (Test-Path $fullPath) {
        Write-Host "Processing $protoFile..."
        
        protoc --go_out=$goOutPath `
               --go_opt=paths=source_relative `
               --go-grpc_out=$goOutPath `
               --go-grpc_opt=paths=source_relative `
               --proto_path=$protoPath `
               $fullPath
        
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Failed to generate code for $protoFile"
            exit 1
        }
        
        Write-Host "  Successfully generated code for $protoFile"
    }
    else {
        Write-Warning "Proto file not found: $fullPath"
    }
}

Write-Host "`nAll gRPC code generated successfully!" -ForegroundColor Green
