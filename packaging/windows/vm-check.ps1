# Проверка установки/удаления JetBox на тестовой ВМ (не на ПК клиентов).
#   vm-check.ps1 -Setup C:\JetBoxSpike\JetBox-Setup.exe -Mode install|uninstall
param([string] $Setup, [ValidateSet('install','uninstall')] [string] $Mode = 'install')
$ErrorActionPreference = 'Continue'
function Row($k, $v) { "{0,-28} {1}" -f $k, $v }

if ($Mode -eq 'install') {
  $p = Start-Process -FilePath $Setup -ArgumentList '/S' -Wait -PassThru
  Row 'exit code' $p.ExitCode
} else {
  $u = 'C:\Program Files\JetBox\Uninstall.exe'
  if (Test-Path $u) {
    $p = Start-Process -FilePath $u -ArgumentList '/S' -Wait -PassThru
    Row 'exit code' $p.ExitCode
    Start-Sleep 5   # деинсталлятор NSIS перезапускает себя из TEMP
  }
}
$svc = Get-CimInstance Win32_Service -Filter "Name='JetBox'" -ErrorAction SilentlyContinue
Row 'service' $(if ($svc) { "$($svc.State) / $($svc.DisplayName) / $($svc.PathName)" } else { 'нет' })
Row 'pipe' (([IO.Directory]::GetFiles('\\.\pipe\') | ? { $_ -match 'jetbox' }) -join ',')
Row 'files' ((Get-ChildItem 'C:\Program Files\JetBox' -ErrorAction SilentlyContinue | % Name) -join ',')
Row 'scheme' (Get-ItemProperty 'HKLM:\Software\Classes\jetbox\shell\open\command' -ErrorAction SilentlyContinue).'(default)'
Row 'run' (Get-ItemProperty 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Run' -ErrorAction SilentlyContinue).JetBox
Row 'uninstall entry' (Get-ItemProperty 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\JetBox' -ErrorAction SilentlyContinue).DisplayName
Row 'firewall' ((Get-NetFirewallRule -ErrorAction SilentlyContinue | ? DisplayName -like 'JetBox*' | % DisplayName) -join ',')
Row 'nrpt' ((Get-ChildItem 'HKLM:\SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig' -ErrorAction SilentlyContinue | ? PSChildName -like 'JetBox-*' | % PSChildName) -join ',')
Row 'adapter' ((Get-NetAdapter -ErrorAction SilentlyContinue | ? Name -eq 'JetBox' | % Status) -join ',')
Row 'ProgramData' (Test-Path 'C:\ProgramData\JetBox')
Row 'official NetBird service' ((Get-Service -Name NetBird -ErrorAction SilentlyContinue).Status)
