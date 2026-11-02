Unicode true
!include MUI2.nsh
!include LogicLib.nsh
!include FileFunc.nsh
!include x64.nsh
!ifndef PAYLOAD
  !error "PAYLOAD is required; use scripts/package-windows.ps1"
!endif
!ifndef OUTPUT
  !error "OUTPUT is required"
!endif
!define VERSION "0.1.0"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\OTONEIGO"
Name "OTONEIGO ${VERSION}"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\OTONEIGO"
RequestExecutionLevel user
ManifestSupportedOS all
CRCCheck force
; Keep the installer stub/compression under zlib; do not pull in CPL LZMA.
SetCompressor zlib
VIProductVersion "0.1.0.0"
VIAddVersionKey /LANG=1033 "ProductName" "OTONEIGO"
VIAddVersionKey /LANG=1033 "FileDescription" "OTONEIGO per-user installer"
VIAddVersionKey /LANG=1033 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Copyright (c) 2026 voici5986"
BrandingText "OTONEIGO"
ShowInstDetails show
ShowUninstDetails show
Var NoIntegration
Var Parameters
Var ExistingMode

!insertmacro MUI_PAGE_LICENSE "${PAYLOAD}\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
LangString CloseApp ${LANG_ENGLISH} "Close OTONEIGO before installing, upgrading or uninstalling."
LangString CloseApp ${LANG_SIMPCHINESE} "请先关闭 OTONEIGO，再安装、更新或卸载。"
LangString InvalidTarget ${LANG_ENGLISH} "The target is not a compatible OTONEIGO installation, or its integration mode differs. Choose another folder."
LangString InvalidTarget ${LANG_SIMPCHINESE} "目标文件夹不是兼容的 OTONEIGO 安装，或系统集成模式不一致。请选择其他文件夹。"

!macro CheckRunning
  ; Window captions are dynamic and auxiliary windows can outlive visibility.
  ; Fail closed if the exact target executable cannot be opened for writing.
  ; OPEN_EXISTING does not truncate or write bytes. Windows denies write access
  ; to a running mapped executable even when all sharing modes are permitted.
  Push $0
  ${If} ${FileExists} "$INSTDIR\OTONEIGO.exe"
    System::Call 'kernel32::CreateFileW(w "$INSTDIR\OTONEIGO.exe", i 0x40000000, i 7, p 0, i 3, i 0x80, p 0) p .r0'
    ${If} $0 P= -1
      Pop $0
      MessageBox MB_OK|MB_ICONEXCLAMATION "$(CloseApp)" /SD IDOK
      SetErrorLevel 2
      Quit
    ${EndIf}
    System::Call 'kernel32::CloseHandle(p r0)'
  ${EndIf}
  Pop $0
!macroend

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "OTONEIGO requires Windows x64." /SD IDOK
    SetErrorLevel 3
    Quit
  ${EndIf}
  SetShellVarContext current
  SetRegView 64
  StrCpy $NoIntegration 0
  ${GetParameters} $Parameters
  ClearErrors
  ${GetOptions} $Parameters "/NOINTEGRATION" $0
  ${IfNot} ${Errors}
    StrCpy $NoIntegration 1
  ${EndIf}
  !insertmacro CheckRunning
FunctionEnd

Section "OTONEIGO" SEC_MAIN
  !insertmacro CheckRunning
  ; Only installations with our marker can be upgraded. Never remove a target tree.
  ${If} ${FileExists} "$INSTDIR\OTONEIGO.exe"
    ReadINIStr $ExistingMode "$INSTDIR\.otoneigo-install.ini" "Install" "NoIntegration"
    ${If} $ExistingMode != $NoIntegration
      MessageBox MB_OK|MB_ICONSTOP "$(InvalidTarget)" /SD IDOK
      SetErrorLevel 4
      Quit
    ${EndIf}
  ${EndIf}
  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "${PAYLOAD}\OTONEIGO.exe"
  File "${PAYLOAD}\LICENSE"
  File "${PAYLOAD}\THIRD_PARTY_NOTICES.txt"
  File "${PAYLOAD}\README.txt"
  WriteINIStr "$INSTDIR\.otoneigo-install.ini" "Install" "NoIntegration" "$NoIntegration"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  ${If} $NoIntegration == 0
    CreateDirectory "$SMPROGRAMS\OTONEIGO"
    CreateShortcut "$SMPROGRAMS\OTONEIGO\OTONEIGO.lnk" "$INSTDIR\OTONEIGO.exe"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "OTONEIGO"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "voici5986"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\OTONEIGO.exe"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  ${EndIf}
  ; No automatic launch, including silent mode.
SectionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
  !insertmacro CheckRunning
  ReadINIStr $NoIntegration "$INSTDIR\.otoneigo-install.ini" "Install" "NoIntegration"
  ${If} $NoIntegration != 0
  ${AndIf} $NoIntegration != 1
    MessageBox MB_OK|MB_ICONSTOP "$(InvalidTarget)" /SD IDOK
    SetErrorLevel 4
    Quit
  ${EndIf}
FunctionEnd

Section "Uninstall"
  !insertmacro CheckRunning
  ${If} $NoIntegration == 0
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
    ${If} $0 == $INSTDIR
      Delete "$SMPROGRAMS\OTONEIGO\OTONEIGO.lnk"
      RMDir "$SMPROGRAMS\OTONEIGO"
      DeleteRegKey HKCU "${UNINSTALL_KEY}"
    ${EndIf}
  ${EndIf}
  Delete "$INSTDIR\OTONEIGO.exe"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
  Delete "$INSTDIR\README.txt"
  Delete "$INSTDIR\.otoneigo-install.ini"
  Delete "$INSTDIR\Uninstall.exe"
  ; Nonrecursive cleanup preserves all unknown files and user data.
  RMDir "$INSTDIR"
SectionEnd
