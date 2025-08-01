module github.com/liangpengcheng/qcontinuum

go 1.12

replace (
	golang.org/x/crypto => github.com/golang/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/net => github.com/golang/net v0.0.0-20190813141303-74dc4d7220e7
	golang.org/x/sys => github.com/golang/sys v0.0.0-20220520151302-bc2c85ada10a
)

require (
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/fasthttp/router v0.5.0
	github.com/fasthttp/websocket v1.4.0
	github.com/gin-gonic/gin v1.9.1
	github.com/golang/protobuf v1.5.0
	github.com/gorilla/websocket v1.4.2
	github.com/klauspost/reedsolomon v1.9.2 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible
	github.com/pkg/errors v0.8.1 // indirect
	github.com/rogpeppe/go-internal v1.8.0 // indirect
	github.com/templexxx/cpufeat v0.0.0-20180724012125-cef66df7f161 // indirect
	github.com/templexxx/xor v0.0.0-20181023030647-4e92f724b73b // indirect
	github.com/tjfoc/gmsm v1.0.1 // indirect
	github.com/valyala/fasthttp v1.34.0
	github.com/xtaci/kcp-go v5.4.4+incompatible
	github.com/xtaci/lossyconn v0.0.0-20190602105132-8df528c0c9ae // indirect
	go.uber.org/zap v1.27.0
	golang.org/x/net v0.10.0
	golang.org/x/sys v0.8.0
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)
