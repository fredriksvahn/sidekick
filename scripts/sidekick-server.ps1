# Sidekick auto-update server
# Pulls latest, builds, and serves. Add to Windows startup.

$repoUrl = "https://github.com/earlysvahn/sidekick.git"
$installDir = "C:\sidekick"
$repoDir = "$installDir\repo"
$logFile = "$installDir\sidekick.log"

# Create install directory
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

#region Logging and Discord

function Log($msg) {
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "$timestamp $msg" | Tee-Object -FilePath $logFile -Append
}

function Get-WebhookUrl {
    # Priority 1: Environment variable
    if ($env:SIDEKICK_DISCORD_WEBHOOK) {
        return $env:SIDEKICK_DISCORD_WEBHOOK
    }
    # Priority 2: Local secrets file
    $secretsFile = "$installDir\secrets.json"
    if (Test-Path $secretsFile) {
        $secrets = Get-Content $secretsFile | ConvertFrom-Json
        if ($secrets.discord_webhook) {
            return $secrets.discord_webhook
        }
    }
    return $null
}

function Send-DiscordMessage($text) {
    $webhookUrl = Get-WebhookUrl
    if (-not $webhookUrl) {
        Log "Discord webhook not configured, skipping notification"
        return
    }
    try {
        $body = @{ content = $text } | ConvertTo-Json -Compress
        Invoke-RestMethod -Uri $webhookUrl -Method Post -ContentType "application/json" -Body $body | Out-Null
        Log "Discord notification sent"
    } catch {
        Log "Failed to send Discord notification: $_"
    }
}

#endregion

#region Main Script

try {
    # Stop any existing instance
    Get-Process -Name "sidekick" -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1

    # Clone or pull
    $wasUpdated = $false
    if (Test-Path "$repoDir\.git") {
        Log "Pulling latest changes..."
        Set-Location $repoDir
        $pullOutput = git pull origin main 2>&1 | Out-String
        Log $pullOutput.Trim()

        if ($LASTEXITCODE -ne 0) {
            throw "git pull failed: $pullOutput"
        }

        # Check if there were actual changes
        if ($pullOutput -notmatch "Already up to date") {
            $wasUpdated = $true
        }
    } else {
        Log "Cloning repository..."
        $cloneOutput = git clone $repoUrl $repoDir 2>&1 | Out-String
        Log $cloneOutput.Trim()

        if ($LASTEXITCODE -ne 0) {
            throw "git clone failed: $cloneOutput"
        }

        Set-Location $repoDir
        $wasUpdated = $true
    }

    # Build
    Log "Building sidekick.exe..."
    $env:CGO_ENABLED = "0"
    $buildOutput = go build -o "$installDir\sidekick.exe" ./cmd/sidekick 2>&1 | Out-String

    if ($buildOutput) {
        Log $buildOutput.Trim()
    }

    if ($LASTEXITCODE -ne 0 -or -not (Test-Path "$installDir\sidekick.exe")) {
        throw "go build failed: $buildOutput"
    }

    # Notify only if updated
    if ($wasUpdated) {
        Send-DiscordMessage "🚀 Sidekick updated and rebuilt successfully"
    }

    # Run server
    Log "Starting server on 0.0.0.0:1337..."
    & "$installDir\sidekick.exe" --serve 2>&1 | ForEach-Object { Log $_ }

    # If we reach here, server exited unexpectedly
    throw "Server process exited unexpectedly"

} catch {
    $errorMsg = $_.Exception.Message
    Log "FATAL: $errorMsg"
    Send-DiscordMessage "❌ Sidekick startup failed: $errorMsg"
    exit 1
}

#endregion
