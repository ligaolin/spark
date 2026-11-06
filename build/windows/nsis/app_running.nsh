# 安装前处理正在运行的实例。
#
# 正在运行的 spark.exe 会被 Windows 锁定，NSIS 覆盖时会弹出
# "Error opening file for writing"，而且这个错误框里的“中止”并不可靠，
# 所以必须在复制文件之前就把旧进程关掉。

!include "LogicLib.nsh"

!macro spark.ensureAppNotRunning
    ${If} ${Silent}
        Call SparkSilentCloseRunningApp
    ${Else}
        Call SparkEnsureAppNotRunning
    ${EndIf}
!macroend

# tasklist 命中同名进程时 find 返回 0，否则返回 1
Function SparkIsAppRunning
    # 用 $SYSDIR 绝对路径调用，避免 PATH 里其它同名工具（如 Git 的 find.exe）抢先
    nsExec::ExecToStack 'cmd /c $SYSDIR\tasklist.exe /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" /NH /FO CSV | $SYSDIR\find.exe /I "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    StrCpy $R0 "0"
    ${If} $0 == "0"
        StrCpy $R0 "1"
    ${EndIf}
FunctionEnd

Function SparkKillApp
    nsExec::Exec 'cmd /c $SYSDIR\taskkill.exe /F /IM "${PRODUCT_EXECUTABLE}" /T >nul 2>&1'
    Sleep 1200
FunctionEnd

Function SparkEnsureAppNotRunning
    Call SparkIsAppRunning
    ${If} $R0 != "1"
        Return
    ${EndIf}

    MessageBox MB_YESNO|MB_ICONQUESTION "${INFO_PRODUCTNAME} 正在运行，需要先关闭它才能继续安装。$\r$\n$\r$\n是否关闭 ${INFO_PRODUCTNAME} 并继续安装？" IDYES sparkCloseApp
    Quit

    sparkCloseApp:
    Call SparkKillApp
    Call SparkIsAppRunning
    ${If} $R0 == "1"
        MessageBox MB_OK|MB_ICONSTOP "无法关闭 ${INFO_PRODUCTNAME}，请手动退出程序（包括托盘图标）后重新运行安装程序。" IDOK
        Quit
    ${EndIf}
FunctionEnd

Function SparkSilentCloseRunningApp
    Call SparkIsAppRunning
    ${If} $R0 != "1"
        Return
    ${EndIf}
    Call SparkKillApp
    Call SparkIsAppRunning
    ${If} $R0 == "1"
        SetErrorLevel 66
        Abort
    ${EndIf}
FunctionEnd
