param(
    [Parameter(Position=0)]
    [ValidateSet("dev", "prod")]
    [string]$env = "dev"
)

$ErrorActionPreference = "Stop"

# Read version from config.go (single source of truth)
$configFile = "internal/config/config.go"
$configContent = Get-Content $configFile -Raw

$versionMajor = if ($configContent -match 'VersionMajor\s*=\s*(\d+)') { $matches[1] } else { "1" }
$versionMinor = if ($configContent -match 'VersionMinor\s*=\s*(\d+)') { $matches[1] } else { "0" }
$versionPatch = if ($configContent -match 'VersionPatch\s*=\s*(\d+)') { $matches[1] } else { "0" }
$versionBuild = 0
$productName = if ($configContent -match 'ProductName\s*=\s*"([^"]+)"') { $matches[1] } else { "Ritual" }
$description = if ($configContent -match 'Description\s*=\s*"([^"]+)"') { $matches[1] } else { "Ritual" }
$companyName = if ($configContent -match 'GroupName\s*=\s*"([^"]+)"') { $matches[1] } else { "k10wl" }

$versionString = "$versionMajor.$versionMinor.$versionPatch.$versionBuild"

Write-Host "Version: $versionString" -ForegroundColor Cyan

# Generate versioninfo.json
$versionInfo = @{
    FixedFileInfo = @{
        FileVersion = @{
            Major = [int]$versionMajor
            Minor = [int]$versionMinor
            Patch = [int]$versionPatch
            Build = [int]$versionBuild
        }
        ProductVersion = @{
            Major = [int]$versionMajor
            Minor = [int]$versionMinor
            Patch = [int]$versionPatch
            Build = [int]$versionBuild
        }
        FileFlagsMask = "3f"
        FileFlags = "00"
        FileOS = "040004"
        FileType = "01"
        FileSubType = "00"
    }
    StringFileInfo = @{
        Comments = ""
        CompanyName = $companyName
        FileDescription = $description
        FileVersion = $versionString
        InternalName = "ritual"
        LegalCopyright = ""
        LegalTrademarks = ""
        OriginalFilename = "ritual.exe"
        PrivateBuild = ""
        ProductName = $productName
        ProductVersion = $versionString
        SpecialBuild = ""
    }
    VarFileInfo = @{
        Translation = @{
            LangID = "0409"
            CharsetID = "04B0"
        }
    }
    IconPath = "../../assets/baiki-$env.ico"
    ManifestPath = ""
}

$versionInfoJson = $versionInfo | ConvertTo-Json -Depth 10
Set-Content -Path "cmd/cli/versioninfo.json" -Value $versionInfoJson

# Read env file
$envFile = ".env.$env.local"
if (-not (Test-Path $envFile)) {
    Write-Error "Environment file not found: $envFile"
    exit 1
}

$envVars = @{}
Get-Content $envFile | ForEach-Object {
    if ($_ -match '^([^#][^=]+)=(.*)$') {
        $envVars[$matches[1].Trim()] = $matches[2].Trim()
    }
}

# Validate required vars
$required = @("R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET_NAME", "APP_NAME")
foreach ($var in $required) {
    if (-not $envVars.ContainsKey($var)) {
        Write-Error "Missing required variable: $var"
        exit 1
    }
}

Write-Host "Building ritual_$env.exe..." -ForegroundColor Cyan

# Generate resources
Write-Host "Generating resources..." -ForegroundColor Gray
Push-Location cmd/cli
go generate
Pop-Location

# Build
Write-Host "Compiling..." -ForegroundColor Gray
$ldflags = "-X main.envAccountID=$($envVars['R2_ACCOUNT_ID']) -X main.envAccessKeyID=$($envVars['R2_ACCESS_KEY_ID']) -X main.envSecretAccessKey=$($envVars['R2_SECRET_ACCESS_KEY']) -X main.envBucket=$($envVars['R2_BUCKET_NAME']) -X ritual/internal/config.AppName=$($envVars['APP_NAME'])"

go build -ldflags $ldflags -o "ritual_$env.exe" ./cmd/cli

if ($LASTEXITCODE -eq 0) {
    Write-Host "Built: ritual_$env.exe" -ForegroundColor Green
} else {
    Write-Error "Build failed"
    exit 1
}
