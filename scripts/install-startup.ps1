# Installs sidekick-server.ps1 to run at Windows startup (hidden)
# Run this once as Administrator

$installDir = "C:\sidekick"
$scriptPath = "$installDir\sidekick-server.ps1"
$taskName = "SidekickServer"

# Copy script to install location
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$scriptUrl = "https://raw.githubusercontent.com/earlysvahn/sidekick/main/scripts/sidekick-server.ps1"
Invoke-WebRequest -Uri $scriptUrl -OutFile $scriptPath

# Remove existing task if present
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

# Create scheduled task that runs at logon
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-WindowStyle Hidden -ExecutionPolicy Bypass -File `"$scriptPath`""
$trigger = New-ScheduledTaskTrigger -AtLogon
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -RunLevel Highest

Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal

Write-Host "Installed! Sidekick will start automatically on login."
Write-Host "To start now: schtasks /run /tn SidekickServer"
Write-Host "To stop: Get-Process sidekick | Stop-Process"
Write-Host "Logs: C:\sidekick\sidekick.log"
