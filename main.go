package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
)

var (
	//命令行参数
	serviceName    string
	testURL        string
	checkInterval  time.Duration
	testTimeout    time.Duration
	skipProcessStr string // 逗号分隔需要跳过清理的进程列表

	maxLogSizeBytes int64 = 5 * 1024 * 1024 //5MB日志轮转
	logger          *log.Logger
)

// isAnySkipProcessRunning 判断是否配置的跳过进程中有任意一个正在运行
func isAnySkipProcessRunning() bool {
	if skipProcessStr == "" {
		return false
	}
	procList := strings.Split(skipProcessStr, ",")
	for _, procName := range procList {
		procName = strings.TrimSpace(procName)
		if procName == "" {
			continue
		}
		if checkProcessExist(procName) {
			logger.Printf("检测到跳过进程 [%s] 正在运行，将跳过代理清理", procName)
			return true
		}
	}
	return false
}

func initLogger() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("get exe path err: %v\n", err)
		logger = log.New(os.Stdout, "", log.LstdFlags)
		return
	}
	logDir := filepath.Dir(exePath)
	logPath := filepath.Join(logDir, "ProxyGuard.log")

	if fi, err := os.Stat(logPath); err == nil {
		if fi.Size() >= maxLogSizeBytes {
			backup := filepath.Join(logDir, fmt.Sprintf("ProxyGuard_%s.log.bak", time.Now().Format("20060102_150405")))
			_ = os.Rename(logPath, backup)
		}
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("open log file failed, use stdout: %v\n", err)
		logger = log.New(os.Stdout, "", log.LstdFlags)
		return
	}

	isSvc, _ := svc.IsWindowsService()
	if !isSvc {
		mw := io.MultiWriter(f, os.Stdout)
		logger = log.New(mw, "", log.LstdFlags)
	} else {
		logger = log.New(f, "", log.LstdFlags)
	}
}

// getInteractiveUserSID 获取当前控制台登录用户SID
func getInteractiveUserSID() (*windows.SID, error) {
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0xFFFFFFFF {
		return nil, fmt.Errorf("no active console session (0xFFFFFFFF)")
	}

	var token windows.Token
	err := windows.WTSQueryUserToken(sessionID, &token)
	if err != nil {
		return nil, fmt.Errorf("WTSQueryUserToken failed: %w", err)
	}
	defer token.Close()

	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("GetTokenUser failed: %w", err)
	}
	return tokenUser.User.Sid, nil
}

// getUserProxySetting 读取登录用户注册表，返回是否开启代理、代理地址
func getUserProxySetting() (proxyEnable int, proxyServer string, err error) {
	sid, err := getInteractiveUserSID()
	if err != nil {
		return 0, "", err
	}
	sidStr := sid.String()
	subKey := fmt.Sprintf(`%s\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, sidStr)

	key, err := registry.OpenKey(registry.USERS, subKey, registry.QUERY_VALUE)
	if err != nil {
		return 0, "", fmt.Errorf("open user reg key failed: %w", err)
	}
	defer key.Close()

	valEnable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil {
		proxyEnable = 0
	} else {
		proxyEnable = int(valEnable)
	}

	valServer, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		proxyServer = ""
	} else {
		proxyServer = valServer
	}
	return proxyEnable, proxyServer, nil
}

// openUserInternetSettingsKey 打开登录用户注册表 Internet Settings（写权限）
func openUserInternetSettingsKey() (registry.Key, error) {
	sid, err := getInteractiveUserSID()
	if err != nil {
		return 0, err
	}
	sidStr := sid.String()
	subKey := fmt.Sprintf(`%s\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, sidStr)
	key, err := registry.OpenKey(registry.USERS, subKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return 0, fmt.Errorf("open USERS\\%s failed: %w", subKey, err)
	}
	return key, nil
}

// checkProcessExist 仅检测进程是否存在，不做杀死
func checkProcessExist(processName string) bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)

	var procEntry windows.ProcessEntry32
	procEntry.Size = uint32(unsafe.Sizeof(procEntry))

	err = windows.Process32First(snapshot, &procEntry)
	if err != nil {
		return false
	}

	for {
		name := windows.UTF16ToString(procEntry.ExeFile[:])
		if name == processName {
			return true
		}
		err = windows.Process32Next(snapshot, &procEntry)
		if err != nil {
			break
		}
	}
	return false
}

// clearSystemProxy 清理代理，检测 clash‑verge.exe 只打警告日志
func clearSystemProxy() error {
	logger.Println("检测到网络异常，准备清理系统代理")

	if checkProcessExist("clash-verge.exe") {
		logger.Println("WARNING: clash-verge.exe is running, may rewrite proxy registry")
	}

	key, err := openUserInternetSettingsKey()
	if err != nil {
		logger.Printf("打开用户注册表失败: %v", err)
		return err
	}
	defer key.Close()

	_ = key.SetDWordValue("ProxyEnable", 0)
	_ = key.DeleteValue("ProxyServer")
	_ = key.DeleteValue("ProxyOverride")
	logger.Println("系统代理注册表清理完成")
	return nil
}

// testProxyConnect：读取登录用户的代理配置，手动构造proxy进行探测，和浏览器行为一致
func testProxyConnect() bool {
	proxyEnable, proxyServer, err := getUserProxySetting()
	if err != nil {
		logger.Printf("读取用户代理配置失败: %v，放弃探测", err)
		return true
	}

	var transport = &http.Transport{}

	if proxyEnable == 1 && proxyServer != "" {
		logger.Printf("当前用户系统代理已开启，代理地址：%s", proxyServer)
		proxyUrl, parseErr := url.Parse("http://" + proxyServer)
		if parseErr == nil {
			transport.Proxy = http.ProxyURL(proxyUrl)
		} else {
			logger.Printf("代理地址解析失败 %v，不探测网络，直接清理系统代理。", parseErr)
			return false
		}
	} else {
		logger.Println("当前用户无开启系统代理，跳过网络探测。")
		return true
	}

	client := http.Client{
		Timeout:   testTimeout,
		Transport: transport,
	}

	resp, err := client.Get(testURL)
	if err != nil {
		logger.Printf("网络探测失败: %v", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Printf("探测返回非200状态码: %s", resp.Status)
		return false
	}
	return true
}

func checkLoop(ctx context.Context) {
	logger.Println("==== ProxyGuard 检测循环启动 ====")
	logger.Printf("参数: testURL=%s checkInterval=%v testTimeout=%v skip-clean-when-has-procs=%s", testURL, checkInterval, testTimeout, skipProcessStr)
	for {
		select {
		case <-ctx.Done():
			logger.Println("检测循环收到退出信号，结束")
			return
		case <-time.After(checkInterval):
			if isAnySkipProcessRunning() {
				logger.Println("存在需要跳过检测的进程，本次放弃检测和清理")
				return
			}
			ok := testProxyConnect()
			if !ok {
				_ = clearSystemProxy()
			}
		}
	}
}

type proxyGuardSvc struct{}

func (m *proxyGuardSvc) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go checkLoop(ctx)

	for c := range r {
		switch c.Cmd {
		case svc.Stop, svc.Shutdown:
			logger.Println("服务收到停止指令，准备退出")
			cancel()
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		case svc.Interrogate:
			changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
		}
	}
	return false, 0
}

func main() {
	//命令行参数定义
	flag.StringVar(&serviceName, "service", "ProxyGuardService", "系统代理异常清理维护服务")
	flag.StringVar(&testURL, "url", "https://www.baidu.com", "探测URL")
	flag.DurationVar(&checkInterval, "interval", 30*time.Second, "检测间隔")
	flag.DurationVar(&testTimeout, "timeout", 8*time.Second, "http超时时间")
	flag.StringVar(&skipProcessStr, "skip-clean-when-has-procs", "", "跳过清理的进程列表，多个用逗号分隔，例：clash-verge.exe,nekoray.exe")
	flag.Parse()

	// 强制切换工作目录到exe所在目录，解决sc服务默认C:\Windows\System32
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		_ = os.Chdir(exeDir)
	}

	initLogger()

	isSvc, err := svc.IsWindowsService()
	if err != nil {
		logger.Fatalf("判断服务模式失败: %v", err)
	}

	if !isSvc {
		logger.Println("运行在控制台调试模式")
		logger.Printf("启动参数 --service=%s --url=%s --interval=%v --timeout=%v --skip-clean-when-has-procs=%s",
			serviceName, testURL, checkInterval, testTimeout, skipProcessStr)
		checkLoop(context.Background())
		return
	}

	logger.Println("作为Windows系统服务启动（LocalSystem账户）")
	logger.Printf("启动参数 --service=%s --url=%s --interval=%v --timeout=%v --skip-clean-when-has-procs=%s",
		serviceName, testURL, checkInterval, testTimeout, skipProcessStr)
	err = svc.Run(serviceName, &proxyGuardSvc{})
	if err != nil {
		logger.Fatalf("服务运行异常: %v", err)
	}
}
