[CmdletBinding()]
param (
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string[]]$ProtoFiles,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$OutDir,

    [ValidateNotNullOrEmpty()]
    [string[]]$ProtoPath = @('.'),

    [string[]]$CSharpOptions = @()
)

$ErrorActionPreference = 'Stop'
$protoc = Join-Path $PSScriptRoot 'protoc.exe'
$includePath = Join-Path $PSScriptRoot 'include'
$outputPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutDir)

$protocArgs = @()
foreach ($path in $ProtoPath) {
    $resolvedPath = (Resolve-Path -LiteralPath $path).ProviderPath
    if (-not (Test-Path -LiteralPath $resolvedPath -PathType Container)) {
        throw "ProtoPath must be a directory: $path"
    }
    $protocArgs += "--proto_path=$resolvedPath"
}
$protocArgs += "--proto_path=$includePath"
$protocArgs += "--csharp_out=$outputPath"
foreach ($option in $CSharpOptions) {
    $protocArgs += "--csharp_opt=$option"
}
foreach ($file in $ProtoFiles) {
    $protocArgs += (Resolve-Path -LiteralPath $file).ProviderPath
}

New-Item -ItemType Directory -Path $outputPath -Force | Out-Null
& $protoc @protocArgs
exit $LASTEXITCODE
