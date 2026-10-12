# Windows Protobuf 代码生成工具

## 版本

| 工具 | 版本 | 用途 |
| --- | --- | --- |
| `win/protoc.exe` | 36.2，Windows x64 | 解析 `.proto`，内置 C# 生成器 |
| `win/protoc-gen-go.exe` | v1.36.12，Windows x64 | 生成使用 `google.golang.org/protobuf` 的 Go 代码 |
| `win/include` | 来自 protoc 36.2 官方发布包 | `google/protobuf/*.proto` 标准定义 |

Go 插件与本仓库 `go.mod` 中的 Go Protobuf 运行时版本一致。`protoc` 和 Go 插件采用独立的版本号。

C# 生成器内置于 `protoc.exe`，使用 `--csharp_out` 即可生成消息类。C# 工程需要引用 **Google.Protobuf 3.36.2**；Unity 工程也需要导入对应的运行时 DLL。工具目录不包含 C# 运行时。

## 生成 Go 代码

以下示例从仓库根目录执行，假定协议放在 `proto/game.proto`，输出目录为 `generated/go`：

```powershell
New-Item -ItemType Directory -Path .\generated\go -Force | Out-Null
$goPlugin = (Resolve-Path .\tools\protobuf\win\protoc-gen-go.exe).ProviderPath
& .\tools\protobuf\win\protoc.exe `
    --proto_path=.\proto `
    --proto_path=.\tools\protobuf\win\include `
    "--plugin=protoc-gen-go=$goPlugin" `
    --go_out=.\generated\go `
    --go_opt=paths=source_relative `
    .\proto\game.proto
```

协议中需声明有效的 Go 导入路径，例如：

```proto
option go_package = "example.com/game/protocol;protocol";
option csharp_namespace = "Game.Protocol";
```

生成的 Go 消息实现 `ProtoReflect()`，可直接用于本仓库的 `SendMessage` 和 `ProtobufCodec`。

## 生成 C# 代码

便捷脚本自动创建输出目录，并加入标准定义的导入目录。路径相对于调用时的工作目录解析；多个文件和导入目录可使用 PowerShell 数组：

```powershell
& .\tools\protobuf\win\generate-csharp.ps1 `
    -ProtoFiles @('.\proto\game.proto') `
    -ProtoPath @('.\proto') `
    -OutDir .\generated\csharp
```

需要生成 `[Serializable]` 属性时，加上 `-CSharpOptions @('serializable')`。文件名默认采用 PascalCase，例如 `game.proto` 生成 `Game.cs`。

也可直接调用内置生成器：

```powershell
New-Item -ItemType Directory -Path .\generated\csharp -Force | Out-Null
& .\tools\protobuf\win\protoc.exe `
    --proto_path=.\proto `
    --proto_path=.\tools\protobuf\win\include `
    --csharp_out=.\generated\csharp `
    .\proto\game.proto
```

这些命令生成 Protobuf 消息类；gRPC 服务代码需要另行配置 gRPC 插件。

Windows 官方编译器在当前系统上无法识别含中文的目录路径。协议、工具和输出目录请使用 ASCII 路径；带空格路径已验证可用。

## 来源与重建

- [protoc 36.2 官方发布](https://github.com/protocolbuffers/protobuf/releases/tag/v36.2)
- [Windows x64 下载包](https://github.com/protocolbuffers/protobuf/releases/download/v36.2/protoc-36.2-win64.zip)
- 官方下载包 SHA-256：`f0c128dc0d8492eceece83bb459a4c0e316764b929ffbf1aa416357fd644edd3`
- [Go 插件 v1.36.12](https://github.com/protocolbuffers/protobuf-go/releases/tag/v1.36.12)
- [C# 生成器官方说明](https://protobuf.dev/reference/csharp/csharp-generated/)

Go 插件由官方模块源码构建，在仓库根目录运行：

```powershell
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go build -mod=readonly -trimpath -ldflags='-s -w' `
    -o .\tools\protobuf\win\protoc-gen-go.exe `
    google.golang.org/protobuf/cmd/protoc-gen-go
```

对应的官方许可证位于 `win/LICENSE.protobuf.txt` 和 `win/LICENSE.protobuf-go.txt`。
