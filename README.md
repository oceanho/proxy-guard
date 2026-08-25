# 关于

因在使用代理软件(如Clash Verge)时由于程序异常（比如直接关机,Task kill 进程）退出，其软件设置
的系统代理无法被清理而无法打开网页的问题，写了这个 ProxyGuard 用于间隔性用当前用户配置的系统代
理进行尝试访问网络，如果发现异常，就清理配置的系统代理。

# Coding

依赖 & Others.

## 依赖
```SHELL
go install github.com/tc-hib/go-winres@latest
```

## 生成 Windows系统下的软件版本
``` shell

go-winres init
# 执行上面的命令后，会生成 winres/ 目录，已经 winres.json 文件，修改 winres.json 中的软件版本/公司名称等信息，然后执行。

go-winres make
```

### 编译 exe
```SHELL
go build -o ProxyGuard.exe -trimpath -buildvcs=false -ldflags="-s -w -buildid=" . 
```

## 安装/卸载
```shell
#1. 安装
打开 cmd（用户管理员权限）
./install_sys_svc.bat # 或者鼠标右键,以为管理员权限运行

# 以上操作完成后，在系统服务里面会有名称为：ProxyGuardService 的服务

# 2. 卸载
打开 cmd（用户管理员权限）
./uninstall_sys_svc.bat # 或者鼠标右键,以为管理员权限运行
```

