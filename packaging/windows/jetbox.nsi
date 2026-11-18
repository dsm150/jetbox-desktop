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
!define ACTIVE_SETUP_KEY "Software\Microsoft\Active Setup\Installed Components\JetBox"
!define SCHEME_KEY "Software\Classes\jetbox"
!define DATA "$COMMONPROGRAMDATA\JetBox"
!define NRPT "SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig"
; NB_DISABLE_UPDATE_SETTINGS=false в окружении службы: переменные NB_* служба читает поверх флагов, а на машинах,
; где официальный NetBird закрыт «managed lock» (netbird_toolset.ps1), в системе стоит NB_DISABLE_UPDATE_SETTINGS=true —
; служба отказывала приложению в SetConfig (интерфейс JetBox, порт 51830), и «Подключить» падало с «Что-то пошло не так».
; --disable-profiles=false явно: замена подключения (1.0.5) заводит новый профиль службы с новым ключом WireGuard,
; а служба 1.0.4 записала disable_profiles: true в ${DATA}\service.json — его не удаляет service uninstall, и новый
; install без флага взял бы сохранённое (cmd/service_params.go applyServiceParams).
; NB_DNS_STATE_FILE — один файл состояния на все профили (как на macOS; путь профиля default): хвосты DNS/NRPT/
; маршрутов любого профиля убирает следующий Up, а не только Up того же профиля.
!define DAEMON_FLAGS '--daemon-addr npipe://jetbox --log-file "${DATA}\client.log" --service-env NB_STATE_DIR="${DATA}" --service-env NB_METRICS_PUSH_ENABLED=false --service-env NB_LOG_MAX_SIZE_MB=5 --service-env NB_DISABLE_UPDATE_SETTINGS=false --service-env NB_DNS_STATE_FILE="${DATA}\state.json" --disable-profiles=false'

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
; Только с последней страницы: при /S (тихое обновление службой) страниц нет — StopOld
; закрывает JetBox.exe, и до следующего входа (автозапуск) или ручного запуска значка нет.
; Апстримное обновление перезапускает только netbird-ui.exe (installer_run_windows.go).
Function LaunchAsUser
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\JetBox.exe"'
FunctionEnd

; Состояние прежней службы (до её остановки): $R2 = Connected, Idle, NeedsLogin… или пусто,
; если службы нет или она не ответила. `status --json` печатает одну строку JSON с полем
; "daemonStatus" (client/status/status.go); фильтр по несуществующему адресу убирает список
; пиров, чтобы поле точно попало в вывод nsExec (не длиннее NSIS_MAX_STRLEN).
Function OldDaemonStatus
  StrCpy $R2 ""
  ${IfNot} ${FileExists} "$INSTDIR\jetboxd.exe"
    Return
  ${EndIf}
  nsExec::ExecToStack /TIMEOUT=15000 '"$INSTDIR\jetboxd.exe" status --json --filter-by-ips 255.255.255.255 --daemon-addr npipe://jetbox'
  Pop $0
  Pop $1
  ${If} $0 != 0
    Return
  ${EndIf}
  StrLen $4 $1
  StrCpy $3 0
  find:
    IntCmp $3 $4 done 0 done
    StrCpy $5 $1 16 $3
    StrCmp $5 '"daemonStatus":"' found
    IntOp $3 $3 + 1
    Goto find
  found:
    IntOp $3 $3 + 16
  word:
    StrCpy $5 $1 1 $3
    StrCmp $5 '"' done
    StrCmp $5 "" done
    StrCpy $R2 "$R2$5"
    IntOp $3 $3 + 1
    Goto word
  done:
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
  ; До записи ключа удаления: была ли установка раньше и стоял ли автозапуск ≤1.0.3 (HKLM, для всех)
  ReadRegStr $R0 HKLM "${UNINST_KEY}" "DisplayVersion"
  ReadRegStr $R1 HKLM "${RUN_KEY}" "JetBox"
  Call OldDaemonStatus
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
  ; То же для самой установки: из окружения `service install` записал бы --disable-update-settings в службу и service.json.
  System::Call 'kernel32::SetEnvironmentVariable(t "NB_DISABLE_UPDATE_SETTINGS", t "false")'
  nsExec::ExecToLog '"$INSTDIR\jetboxd.exe" service install ${DAEMON_FLAGS}'
  Pop $0
  nsExec::ExecToLog '"$INSTDIR\jetboxd.exe" service start'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION "$(ServiceFailed)" /SD IDOK
  ${EndIf}

  ; Защита была выключена: ≤1.0.3 не запоминал выключение (флаг автоподключения в профиле не
  ; ставился), и перезапущенная служба подключилась бы сама — опускаем её. Флаг по состоянию
  ; выставит приложение при первом запуске (метка autoconnect-synced). Служба подключается
  ; ещё в Start, до приёма команд, — down её застанет; повтор — пока канал не открылся.
  ${If} $R2 != ""
  ${AndIf} $R2 != "Connected"
  ${AndIf} $R2 != "Connecting"
    StrCpy $3 0
    down_retry:
      nsExec::Exec /TIMEOUT=30000 '"$INSTDIR\jetboxd.exe" down --daemon-addr npipe://jetbox'
      Pop $0
      IntOp $3 $3 + 1
      ${If} $0 != 0
      ${AndIf} $3 < 3
        Sleep 1000
        Goto down_retry
      ${EndIf}
  ${EndIf}

  ; Схема jetbox:// — для всех пользователей
  WriteRegStr HKLM "${SCHEME_KEY}" "" "URL:JetBox"
  WriteRegStr HKLM "${SCHEME_KEY}" "URL Protocol" ""
  WriteRegStr HKLM "${SCHEME_KEY}\DefaultIcon" "" '"$INSTDIR\JetBox.exe",0'
  WriteRegStr HKLM "${SCHEME_KEY}\shell\open\command" "" '"$INSTDIR\JetBox.exe" "%1"'

  ; Автозапуск при входе — только значок (--autostart). У каждого пользователя свой (HKCU\…\Run),
  ; его включает и выключает приложение («Настройки»). Свежая установка — включаем текущему
  ; пользователю; ≤1.0.3 писал HKLM для всех — переносим в HKCU; обновление с ≥1.0.4 — выбор
  ; человека не трогаем. Другим пользователям Windows автозапуск включит само приложение при
  ; первом запуске (метка в %APPDATA%\JetBox). Значение — как у Wails: "<путь>" --autostart.
  ${If} $R1 != ""
  ${OrIf} $R0 == ""
    DeleteRegValue HKLM "${RUN_KEY}" "JetBox"
    WriteRegStr HKCU "${RUN_KEY}" "JetBox" '"$INSTDIR\JetBox.exe" --autostart'
  ${EndIf}
  ; Active Setup: при первом входе каждого пользователя Windows (и того, кто ставил, — HKCU
  ; установщика может быть чужим: UAC другой учёткой, тихое обновление от SYSTEM) Windows до
  ; появления рабочего стола запускает JetBox.exe --autostart-init: он включает автозапуск,
  ; если человек его не выключал (метка autostart-initialized), и сразу выходит.
  WriteRegStr HKLM "${ACTIVE_SETUP_KEY}" "" "JetBox"
  WriteRegStr HKLM "${ACTIVE_SETUP_KEY}" "Version" "1,0"
  WriteRegStr HKLM "${ACTIVE_SETUP_KEY}" "StubPath" '"$INSTDIR\JetBox.exe" --autostart-init'
  WriteRegDWORD HKLM "${ACTIVE_SETUP_KEY}" "IsInstalled" 1

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
  ; Прямой вызов: служба ещё жива, сама ждёт management не дольше 15 с; /TIMEOUT — страховка.
  ; (Раньше здесь была обёртка PowerShell с '' внутри строки в одинарных кавычках — NSIS резал её
  ; на части, и снятие с учёта не запускалось вовсе; packaging/windows/lint.sh это ловит.)
  nsExec::ExecToLog /TIMEOUT=20000 '"$INSTDIR\jetboxd.exe" deregister --daemon-addr npipe://jetbox'
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
  ; Автозапуск текущего пользователя и, на всякий случай, старый общий (≤1.0.3)
  DeleteRegValue HKCU "${RUN_KEY}" "JetBox"
  DeleteRegValue HKLM "${RUN_KEY}" "JetBox"
  DeleteRegKey HKLM "${ACTIVE_SETUP_KEY}"
  DeleteRegKey HKLM "${SCHEME_KEY}"
  DeleteRegKey HKLM "${UNINST_KEY}"

  ; Настройки приложения текущего пользователя (%APPDATA%\JetBox)
  SetShellVarContext current
  RMDir /r "$APPDATA\JetBox"
  SetShellVarContext all
SectionEnd
