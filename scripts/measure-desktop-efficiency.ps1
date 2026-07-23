param(
    [Parameter(Mandatory = $true)]
    [ValidateRange(1, 2147483647)]
    [int]$RootPid,

    [Parameter(Mandatory = $true)]
    [ValidateSet("foreground-idle", "background-idle", "stream-static", "stream-playback")]
    [string]$Scenario,

    [ValidateRange(5, 300)]
    [int]$Seconds = 15,

    [string]$OutputPath
)

$ErrorActionPreference = "Stop"
$sampleIntervalSeconds = 1
$startedAt = [DateTimeOffset]::UtcNow
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $stamp = $startedAt.ToString("yyyyMMdd-HHmmss")
    $OutputPath = Join-Path $PSScriptRoot "..\desktop\e2e\artifacts\efficiency-$Scenario-$stamp.json"
}
$resolvedOutput = [System.IO.Path]::GetFullPath($OutputPath)
$artifactRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\desktop\e2e\artifacts"))
$artifactPrefix = $artifactRoot.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
if (-not $resolvedOutput.StartsWith($artifactPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputPath must stay under desktop\e2e\artifacts."
}
[System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($resolvedOutput)) | Out-Null

function Get-ProcessTree {
    param([int]$ParentPid)
    $all = @(Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name, CommandLine)
    $ids = [System.Collections.Generic.HashSet[int]]::new()
    [void]$ids.Add($ParentPid)
    do {
        $changed = $false
        foreach ($process in $all) {
            if ($ids.Contains([int]$process.ParentProcessId) -and $ids.Add([int]$process.ProcessId)) {
                $changed = $true
            }
        }
    } while ($changed)
    return @($all | Where-Object { $ids.Contains([int]$_.ProcessId) })
}

function Get-Role {
    param($Process, [int]$ParentPid)
    if ([int]$Process.ProcessId -eq $ParentPid) { return "electron-main" }
    $commandLine = [string]$Process.CommandLine
    if ($commandLine -match "--type=gpu-process") { return "electron-gpu" }
    if ($commandLine -match "--type=renderer") { return "electron-renderer" }
    if ($commandLine -match "server\.js") { return "next-server" }
    if ($commandLine -match "codex") { return "codex-agent" }
    if ([string]$Process.Name -match "^zv-orchestrator") { return "orchestrator" }
    return "other-child"
}

function Get-Percentile {
    param([double[]]$Values, [double]$Percentile)
    if ($Values.Count -eq 0) { return 0.0 }
    $ordered = @($Values | Sort-Object)
    $index = [Math]::Ceiling(($Percentile / 100.0) * $ordered.Count) - 1
    return [Math]::Round([double]$ordered[[Math]::Max(0, $index)], 3)
}

function Get-NumberOrZero {
    param($Value)
    if ($null -eq $Value) { return 0 }
    return $Value
}

$samples = [System.Collections.Generic.List[object]]::new()
$previousCpu = @{}
$previousSampleSeconds = $null
$sampleClock = [System.Diagnostics.Stopwatch]::StartNew()
for ($sampleIndex = 0; $sampleIndex -le $Seconds; $sampleIndex += 1) {
    $targetSeconds = $sampleIndex * $sampleIntervalSeconds
    $remainingMilliseconds = [Math]::Floor(($targetSeconds - $sampleClock.Elapsed.TotalSeconds) * 1000)
    if ($remainingMilliseconds -gt 0) { Start-Sleep -Milliseconds $remainingMilliseconds }
    $tree = @(Get-ProcessTree -ParentPid $RootPid)
    if ($tree.Count -eq 0) { throw "The process tree rooted at PID $RootPid no longer exists." }
    $ids = @($tree | ForEach-Object { [int]$_.ProcessId })
    $processes = @(Get-Process -Id $ids -ErrorAction SilentlyContinue)
    $sampleSeconds = $sampleClock.Elapsed.TotalSeconds
    $elapsedSeconds = if ($null -eq $previousSampleSeconds) { 1.0 } else { [Math]::Max(0.001, $sampleSeconds - $previousSampleSeconds) }
    $previousSampleSeconds = $sampleSeconds
    $gpuByPid = @{}
    $gpuMemoryByPid = @{}
    try {
        $counters = (Get-Counter '\GPU Engine(*)\Utilization Percentage','\GPU Process Memory(*)\Dedicated Usage','\GPU Process Memory(*)\Shared Usage' -ErrorAction Stop).CounterSamples
        foreach ($counter in $counters) {
            if ($counter.InstanceName -notmatch "pid_(\d+)") { continue }
            $pidValue = [int]$Matches[1]
            if (-not $ids.Contains($pidValue)) { continue }
            if ($counter.Path -match "Utilization Percentage") {
                $gpuByPid[$pidValue] = [double](Get-NumberOrZero $gpuByPid[$pidValue]) + [double]$counter.CookedValue
            } else {
                $gpuMemoryByPid[$pidValue] = [double](Get-NumberOrZero $gpuMemoryByPid[$pidValue]) + [double]$counter.CookedValue
            }
        }
    } catch {
        # GPU counters are absent on some Windows/RDP configurations; retain a
        # valid sample with zero GPU values so runs remain comparable.
    }

    $roles = @{}
    foreach ($process in $tree) { $roles[[int]$process.ProcessId] = Get-Role -Process $process -ParentPid $RootPid }
    $roleTotals = @{}
    $cpuTotal = 0.0
    $workingSet = 0L
    $privateBytes = 0L
    foreach ($process in $processes) {
        $pidValue = [int]$process.Id
        $cpuNow = [double](Get-NumberOrZero $process.CPU)
        $cpuDelta = if ($previousCpu.ContainsKey($pidValue)) { [Math]::Max(0, $cpuNow - [double]$previousCpu[$pidValue]) } else { 0 }
        $previousCpu[$pidValue] = $cpuNow
        $cpuPercent = 100.0 * $cpuDelta / $elapsedSeconds / [Environment]::ProcessorCount
        $cpuTotal += $cpuPercent
        $workingSet += [long]$process.WorkingSet64
        $privateBytes += [long]$process.PrivateMemorySize64
        $role = [string]$roles[$pidValue]
        if (-not $roleTotals.ContainsKey($role)) {
            $roleTotals[$role] = [ordered]@{ cpu_percent = 0.0; working_set_bytes = 0L; private_bytes = 0L; gpu_percent = 0.0; gpu_memory_bytes = 0L; process_count = 0 }
        }
        $roleTotals[$role].cpu_percent += $cpuPercent
        $roleTotals[$role].working_set_bytes += [long]$process.WorkingSet64
        $roleTotals[$role].private_bytes += [long]$process.PrivateMemorySize64
        $roleTotals[$role].gpu_percent += [double](Get-NumberOrZero $gpuByPid[$pidValue])
        $roleTotals[$role].gpu_memory_bytes += [long](Get-NumberOrZero $gpuMemoryByPid[$pidValue])
        $roleTotals[$role].process_count += 1
    }
    $samples.Add([ordered]@{
        offset_seconds = [Math]::Round($sampleSeconds, 3)
        cpu_percent = [Math]::Round($cpuTotal, 3)
        working_set_bytes = $workingSet
        private_bytes = $privateBytes
        gpu_percent = [Math]::Round([double](Get-NumberOrZero ($gpuByPid.Values | Measure-Object -Sum).Sum), 3)
        gpu_memory_bytes = [long](Get-NumberOrZero ($gpuMemoryByPid.Values | Measure-Object -Sum).Sum)
        roles = $roleTotals
    })
}

$measured = if ($samples.Count -gt 1) { @($samples | Select-Object -Skip 1) } else { @($samples) }
$elapsedSeconds = if ($measured.Count -gt 0) { [double]$measured[-1].offset_seconds } else { 0.0 }
$document = [ordered]@{
    schema_version = 1
    scenario = $Scenario
    root_pid = $RootPid
    started_at = $startedAt.ToString("O")
    target_sample_interval_seconds = $sampleIntervalSeconds
    sample_count = $measured.Count
    elapsed_seconds = $elapsedSeconds
    summary = [ordered]@{
        cpu_p95_percent = Get-Percentile -Values @($measured.cpu_percent) -Percentile 95
        gpu_p95_percent = Get-Percentile -Values @($measured.gpu_percent) -Percentile 95
        working_set_peak_bytes = [long](Get-NumberOrZero ($measured.working_set_bytes | Measure-Object -Maximum).Maximum)
        private_bytes_peak = [long](Get-NumberOrZero ($measured.private_bytes | Measure-Object -Maximum).Maximum)
        gpu_memory_peak_bytes = [long](Get-NumberOrZero ($measured.gpu_memory_bytes | Measure-Object -Maximum).Maximum)
    }
    samples = $measured
}
$document | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $resolvedOutput -Encoding utf8
Write-Output $resolvedOutput
