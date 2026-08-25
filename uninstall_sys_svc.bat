@echo off
chcp 65001 >nul
set "SERVICE_NAME=ProxyGuardService"

echo 准备卸载服务：%SERVICE_NAME%

::先停止服务
sc query "%SERVICE_NAME%" >nul 2>&1
if %errorlevel% equ 0 (
    echo 正在停止服务
    sc stop "%SERVICE_NAME%"
    timeout /t 2 /nobreak >nul
)

echo 删除服务
sc delete "%SERVICE_NAME%"

echo 已执行删除，请检查上面输出确认结果
pause
