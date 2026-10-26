; Установщик JetBox для Windows (NSIS 3, Unicode). Сборка — packaging/windows/build.sh.
Unicode True
ManifestDPIAware true
RequestExecutionLevel admin
SetCompressor /SOLID lzma

!ifndef BUILD
  !define BUILD 0
!endif
!ifndef VERSION
  !error "нужно -DVERSION=X.Y.Z"
!endif
!ifndef SRC
  !error "нужно -DSRC=<каталог с файлами>"
!endif
!ifndef OUT
  !define OUT "JetBox-Setup.exe"
!endif

!define APP "JetBox"
!define COMPANY "JetNet"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\JetBox"
!define RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"
!define SCHEME_KEY "Software\Classes\jetbox"
!define DATA "$COMMONPROGRAMDATA\JetBox"
!define NRPT "SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig"
!define DAEMON_FLAGS '--daemon-addr npipe://jetbox --log-file "${DATA}\client.log" --service-env NB_STATE_DIR="${DATA}" --service-env NB_METRICS_PUSH_ENABLED=false --service-env NB_LOG_MAX_SIZE_MB=5 --disable-profiles'

Name "${APP}"
Caption "${APP}"
OutFile "${OUT}"
InstallDir "$PROGRAMFILES64\JetBox"
BrandingText "JetBox · jet-net.net"
ShowInstDetails nevershow
ShowUninstDetails nevershow

VIProductVersion "${VERSION}.${BUILD}"
VIAddVersionKey /LANG=0 "ProductName" "JetBox"
VIAddVersionKey /LANG=0 "CompanyName" "${COMPANY}"
VIAddVersionKey /LANG=0 "FileDescription" "JetBox Setup"
VIAddVersionKey /LANG=0 "FileVersion" "${VERSION}.${BUILD}"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}.${BUILD}"
VIAddVersionKey /LANG=0 "LegalCopyright" "© 2026 JetNet"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "WinMessages.nsh"

!define MUI_ICON "${SRC}\icon.ico"
!define MUI_UNICON "${SRC}\icon.ico"
!define MUI_WELCOMEPAGE_TITLE "$(WelcomeTitle)"
!define MUI_WELCOMEPAGE_TEXT "$(WelcomeText)"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "$(FinishRun)"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsUser

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "English"
!include "lang.nsh"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "$(Not64)" /SD IDOK
    Abort
  ${EndIf}
  SetRegView 64
  SetShellVarContext all
FunctionEnd

Function un.onInit
  SetRegView 64
  SetShellVarContext all
FunctionEnd

; Запуск приложения от обычного пользователя: explorer.exe открывает файл в контексте
; вошедшего пользователя, а не от администратора, под которым работает установщик.
Function LaunchAsUser
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\JetBox.exe"'
FunctionEnd

!macro StopOld
  DetailPrint "$(StepStop)"
  nsExec::Exec 'taskkill /im JetBox.exe /f'
  Pop $0
  ${If} ${FileExists} "$INSTDIR\jetboxd.exe"
    nsExec::Exec '"$INSTDIR\jetboxd.exe" service stop'
    Pop $0
    nsExec::Exec '"$INSTDIR\jetboxd.exe" service uninstall'
    Pop $0
    Sleep 2000
  ${EndIf}
!macroend

Section "JetBox" SecMain
  SectionIn RO
  !insertmacro StopOld

  SetOutPath "$INSTDIR"
  File "${SRC}\JetBox.exe"
  File "${SRC}\jetboxd.exe"
  File "${SRC}\wintun.dll"
  File "${SRC}\LICENSE.txt"
  File "${SRC}\NOTICE.txt"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  ; WebView2 — только если его нет (макрос по образцу установщика апстрима)
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    DetailPrint "$(StepWebView)"
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "${SRC}\MicrosoftEdgeWebview2Setup.exe"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install'
    SetOutPath "$INSTDIR"
  ${EndIf}

  DetailPrint "$(StepService)"
  CreateDirectory "${DATA}"
  nsExec::ExecToLog '"$INSTDIR\jetboxd.exe" service install ${DAEMON_FLAGS}'
  Pop $0
  nsExec::ExecToLog '"$INSTDIR\jetboxd.exe" service start'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION "$(ServiceFailed)" /SD IDOK
  ${EndIf}

  ; Схема jetbox:// — для всех пользователей
  WriteRegStr HKLM "${SCHEME_KEY}" "" "URL:JetBox"
  WriteRegStr HKLM "${SCHEME_KEY}" "URL Protocol" ""
  WriteRegStr HKLM "${SCHEME_KEY}\DefaultIcon" "" '"$INSTDIR\JetBox.exe",0'
  WriteRegStr HKLM "${SCHEME_KEY}\shell\open\command" "" '"$INSTDIR\JetBox.exe" "%1"'

  ; Автозапуск при входе — только значок (--autostart)
  WriteRegStr HKLM "${RUN_KEY}" "JetBox" '"$INSTDIR\JetBox.exe" --autostart'

  CreateShortCut "$SMPROGRAMS\JetBox.lnk" "$INSTDIR\JetBox.exe"

  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "JetBox"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${COMPANY}"
  WriteRegStr HKLM "${UNINST_KEY}" "URLInfoAbout" "https://jet-net.net"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" '"$INSTDIR\JetBox.exe",0'
  WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /im JetBox.exe /f'
  Pop $0

  ; Снять компьютер с учёта, чтобы место в JetBox освободилось. Не дольше 15 с (офлайн — не ждём).
  DetailPrint "$(StepDeregister)"
  nsExec::Exec 'powershell -NoProfile -NonInteractive -Command "$$p = Start-Process -FilePath ''$INSTDIR\jetboxd.exe'' -ArgumentList ''deregister'',''--daemon-addr'',''npipe://jetbox'' -WindowStyle Hidden -PassThru; if (-not $$p.WaitForExit(15000)) { $$p.Kill() }"'
  Pop $0

  nsExec::Exec '"$INSTDIR\jetboxd.exe" service stop'
  Pop $0
  nsExec::Exec '"$INSTDIR\jetboxd.exe" service uninstall'
  Pop $0
  Sleep 2000

  DetailPrint "$(StepCleanup)"
  nsExec::Exec 'netsh advfirewall firewall delete rule name=JetBox'
  Pop $0
  nsExec::Exec 'netsh advfirewall firewall delete rule name=JetBox-v6'
  Pop $0
  ; Ключи NRPT JetBox-* (обычно служба убирает их сама при остановке)
  StrCpy $1 0
  nrpt_loop:
    EnumRegKey $2 HKLM "${NRPT}" $1
    StrCmp $2 "" nrpt_done
    StrCpy $3 $2 7
    ${If} $3 == "JetBox-"
      DeleteRegKey HKLM "${NRPT}\$2"
    ${Else}
      IntOp $1 $1 + 1
    ${EndIf}
    Goto nrpt_loop
  nrpt_done:

  Delete "$INSTDIR\JetBox.exe"
  Delete "$INSTDIR\jetboxd.exe"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\NOTICE.txt"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  RMDir /r "${DATA}"

  Delete "$SMPROGRAMS\JetBox.lnk"
  DeleteRegValue HKLM "${RUN_KEY}" "JetBox"
  DeleteRegKey HKLM "${SCHEME_KEY}"
  DeleteRegKey HKLM "${UNINST_KEY}"

  ; Настройки приложения текущего пользователя (%APPDATA%\JetBox)
  SetShellVarContext current
  RMDir /r "$APPDATA\JetBox"
  SetShellVarContext all
SectionEnd
