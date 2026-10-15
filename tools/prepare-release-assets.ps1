param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$ZipPath,
    [string]$OutputDirectory = (Join-Path $PSScriptRoot '..\release-assets')
)

$ErrorActionPreference = 'Stop'
$inputs = @(
    [pscustomobject]@{ Source = (Get-Item -LiteralPath $ExePath); Name = 'oppo-virtual-adapter-repair-windows-amd64.exe'; Extension = '.exe' },
    [pscustomobject]@{ Source = (Get-Item -LiteralPath $ZipPath); Name = 'oppo-virtual-adapter-repair-windows-amd64-full.zip'; Extension = '.zip' }
)
foreach ($entry in $inputs) {
    if ($entry.Source.PSIsContainer -or $entry.Source.Extension -ne $entry.Extension) {
        throw ('发布输入必须是对应类型的文件：' + $entry.Source.FullName)
    }
}

$outputPath = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $outputPath -Force | Out-Null
$checksumLines = @(foreach ($entry in $inputs) {
    $destination = Join-Path $outputPath $entry.Name
    $expected = (Get-FileHash -LiteralPath $entry.Source.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($entry.Source.FullName -ne $destination) {
        Copy-Item -LiteralPath $entry.Source.FullName -Destination $destination -Force
    }
    if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
        throw ('发布附件复制校验失败：' + $entry.Name)
    }
    $expected + '  ' + $entry.Name
})
[IO.File]::WriteAllText(
    (Join-Path $outputPath 'SHA256-latest.txt'),
    ($checksumLines -join "`n") + "`n",
    [Text.UTF8Encoding]::new($false)
)
Get-Item -LiteralPath (Join-Path $outputPath $inputs[0].Name), (Join-Path $outputPath $inputs[1].Name), (Join-Path $outputPath 'SHA256-latest.txt') |
    Select-Object Name, Length
