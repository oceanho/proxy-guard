@echo off
chcp 65001 >nul
:: 获取bat脚本所在目录，末尾自带反斜杠
set "BASE_DIR=%~dp0"
set "EXE_PATH=%BASE_DIR%ProxyGuard.exe"
set "SERVICE_NAME=ProxyGuardService"

echo ==============================================
echo 当前脚本目录: %BASE_DIR%
echo EXE完整路径: %EXE_PATH%
echo ==============================================

:: 判断exe是否存在
if not exist "%EXE_PATH%" (
    echo 错误：找不到 ProxyGuard.exe !
    echo 请把bat和ProxyGuard.exe放在同一个目录
    pause
    exit /b 1
)

::检测服务是否存在
sc query "%SERVICE_NAME%" >nul 2>&1
if %errorlevel% equ 0 (
    echo 服务[%SERVICE_NAME%]已经存在，跳过创建
) else (
    echo 正在创建服务 %SERVICE_NAME%
    :: binPath= 等号后面必须有空格！
    sc create "%SERVICE_NAME%" binPath= "\"%EXE_PATH%\" --url=https://www.baidu.com --interval=30s --timeout=8s --skip-clean-when-has-procs=\"clash-verge.exe,nekoray.exe,v2ray.exe\"" start= auto displayname= "Proxy Guard Service"
    sc description "%SERVICE_NAME%" "代理监护服务，网络异常自动清理系统代理"
    echo 服务创建完成
)

echo 正在启动服务
sc start "%SERVICE_NAME%"
sc query "%SERVICE_NAME%"

echo.
echo 操作完成
pause
