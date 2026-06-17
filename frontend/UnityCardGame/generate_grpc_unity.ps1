$ErrorActionPreference = "Stop"

Write-Host "Generating gRPC code for Unity..."

$protoPath = "../../backend/proto"
$unityOutPath = "./Assets/Scripts/Networking/Proto"

if (-not (Test-Path $unityOutPath)) {
    New-Item -ItemType Directory -Path $unityOutPath -Force | Out-Null
}

$protoFiles = @(
    "game.proto",
    "matchmaking.proto"
)

foreach ($protoFile in $protoFiles) {
    $fullPath = Join-Path $protoPath $protoFile
    if (Test-Path $fullPath) {
        Write-Host "Processing $protoFile..."
        
        protoc --csharp_out=$unityOutPath `
               --grpc_out=$unityOutPath `
               --plugin=protoc-gen-grpc=./Packages/Grpc.Tools/tools/windows_x64/grpc_csharp_plugin.exe `
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

Write-Host "`nFixing namespace..." -ForegroundColor Yellow

$files = Get-ChildItem $unityOutPath -Filter "*.cs"
foreach ($file in $files) {
    $content = Get-Content $file.FullName -Raw
    $content = $content -replace "namespace Cardgame\.Proto", "namespace CardGame.Proto"
    Set-Content -Path $file.FullName -Value $content -NoNewline
    Write-Host "  Updated namespace in $($file.Name)"
}

Write-Host "`nAll gRPC code generated successfully!" -ForegroundColor Green
