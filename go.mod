module github.com/liangpengcheng/qcontinuum

go 1.24.0

toolchain go1.24.11

replace (
	golang.org/x/crypto => github.com/golang/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/net => github.com/golang/net v0.0.0-20190813141303-74dc4d7220e7
	golang.org/x/sys => github.com/golang/sys v0.0.0-20220520151302-bc2c85ada10a
)

require (
	github.com/fasthttp/router v0.5.0
	github.com/fasthttp/websocket v1.4.0
	github.com/gin-gonic/gin v1.9.1
	github.com/golang/protobuf v1.5.0
	github.com/gorilla/websocket v1.4.2
	github.com/natefinch/lumberjack v2.0.0+incompatible
	github.com/valyala/fasthttp v1.34.0
	github.com/xtaci/kcp-go/v5 v5.6.55
	go.uber.org/zap v1.27.0
	golang.org/x/net v0.47.0
	golang.org/x/sys v0.38.0
)

require (
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/andybalholm/brotli v1.0.4 // indirect
	github.com/bytedance/sonic v1.9.1 // indirect
	github.com/chenzhuoyu/base64x v0.0.0-20221115062448-fe3a3abad311 // indirect
	github.com/gabriel-vasile/mimetype v1.4.2 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.14.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.15.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/klauspost/reedsolomon v1.12.6 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/leodido/go-urn v1.2.4 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rogpeppe/go-internal v1.8.0 // indirect
	github.com/savsgio/gotils v0.0.0-20190714152828-365999d0a274 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.11 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/xtaci/lossyconn v1.0.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/arch v0.3.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
