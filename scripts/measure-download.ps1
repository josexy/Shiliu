# Build once, then isolate each scenario. GetProcessTimes inside the test
# measures the downloader plus its local server. No external site or user data.
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
$previousMeasure = $env:DOWNLOADER_MEASURE
try {
    $outputDir = Join-Path $repo '.mygo'
    New-Item -ItemType Directory -Force -Path $outputDir | Out-Null
    $binary = Join-Path $outputDir 'download-measure.exe'
    go test -c -o $binary ./internal/download
    if ($LASTEXITCODE -ne 0) { throw 'measurement build failed' }
    $env:DOWNLOADER_MEASURE = '1'
    foreach ($scenario in @('single-unlimited', 'multi-unlimited', 'single-server-limited', 'multi-server-limited')) {
        $log = Join-Path $outputDir ('measure-' + $scenario + '.txt')
        $errorLog = Join-Path $outputDir ('measure-' + $scenario + '-error.txt')
        $arguments = @('-test.v', ('-test.run=^TestLocalTransferMeasurement$/' + $scenario + '$'), '-test.count=1')
        $process = Start-Process -FilePath $binary -ArgumentList $arguments -PassThru -WindowStyle Hidden -RedirectStandardOutput $log -RedirectStandardError $errorLog
        $null = $process.Handle
        $process.WaitForExit()
        Get-Content -LiteralPath $log | Select-String '^MEASURE' | ForEach-Object { $_.Line }
        if ($process.ExitCode -ne 0) { Get-Content -LiteralPath $errorLog; throw "measurement failed: $scenario" }
    }
} finally {
    $env:DOWNLOADER_MEASURE = $previousMeasure
    Pop-Location
}
