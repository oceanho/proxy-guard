goversioninfo version.json
set CGO_ENABLED=0
go build -o ProxyGuard.exe -trimpath -buildvcs=false -ldflags="-s -w -buildid=" .
