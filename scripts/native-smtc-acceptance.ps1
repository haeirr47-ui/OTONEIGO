# Windows PowerShell 5.1: observe only unless -Control is explicitly supplied.
# Run only while the isolated native acceptance fixture is playing.
param([switch]$Control)
$ErrorActionPreference = 'Stop'
# Construct the fixed title without non-ASCII source text for PowerShell 5.1.
$fixtureTitle = 'OTONEIGO ' + (-join [char[]](0x7F13, 0x5B58, 0x539F, 0x751F, 0x9A8C, 0x6536))
$nextTitle = $fixtureTitle + ' ' + [char]0x00B7 + ' ' + (-join [char[]](0x4E0B, 0x4E00, 0x9996))
$stage = 'initialize'
function Emit($Value) { $Value | ConvertTo-Json -Compress -Depth 5 }
try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $managerType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType=WindowsRuntime]
    $propertiesType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType=WindowsRuntime]
    $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.IsGenericMethodDefinition -and
        $_.GetGenericArguments().Count -eq 1 -and $_.GetParameters().Count -eq 1 -and
        $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1'
    } | Select-Object -First 1
    if ($null -eq $asTask) { throw 'WinRT AsTask overload unavailable' }
    function Await-WinRT($Operation, [Type]$ResultType) {
        $task = $asTask.MakeGenericMethod($ResultType).Invoke($null, @($Operation))
        if (-not $task.Wait(10000)) { throw [TimeoutException]::new('WinRT operation timed out') }
        $task.Result
    }
    $stage = 'RequestAsync'
    $manager = Await-WinRT ($managerType::RequestAsync()) $managerType
    Emit @{ stage=$stage; outcome='available' }
    $script:fixtureSession = $null
    function Get-FixtureSession([string]$ExpectedTitle) {
        $session = $manager.GetCurrentSession()
        # Windows may release current-session focus when playback pauses. Reuse
        # only the first validated fixture session, never enumerate alternatives.
        if ($null -eq $session) { $session = $script:fixtureSession }
        if ($null -eq $session) { throw [InvalidOperationException]::new('no_current_session') }
        $properties = Await-WinRT ($session.TryGetMediaPropertiesAsync()) $propertiesType
        # Do not enumerate sessions or expose metadata from another application.
        if ($properties.Title -cne $ExpectedTitle) { throw [InvalidOperationException]::new('fixture_title_mismatch') }
        if ($null -eq $script:fixtureSession) { $script:fixtureSession = $session }
        $session
    }
    function Observe-Fixture([string]$ExpectedTitle) {
        $session = Get-FixtureSession $ExpectedTitle
        $info = $session.GetPlaybackInfo()
        @{ titleMatched=$true; playbackStatus=$info.PlaybackStatus.ToString();
           canPause=$info.Controls.IsPauseEnabled; canPlay=$info.Controls.IsPlayEnabled;
           canNext=$info.Controls.IsNextEnabled }
    }
    $stage = 'observe'
    Emit @{ stage=$stage; outcome='matched'; observation=(Observe-Fixture $fixtureTitle) }
    if ($Control) {
        foreach ($action in @('Pause', 'Play', 'SkipNext')) {
            $stage = 'Try' + $action + 'Async'
            # Reacquire and validate the current session immediately before each command.
            $session = Get-FixtureSession $fixtureTitle
            $method = 'Try' + $action + 'Async'
            $accepted = Await-WinRT ($session.$method()) ([bool])
            Emit @{ stage=$stage; outcome='returned'; accepted=$accepted }
            if (-not $accepted) { throw [InvalidOperationException]::new('control_rejected') }
            $expectedTitle = $fixtureTitle
            $expectedStatus = if ($action -eq 'Pause') { 'Paused' } else { 'Playing' }
            if ($action -eq 'SkipNext') { $expectedTitle = $nextTitle }
            $observed = $false
            for ($attempt=0; $attempt -lt 20; $attempt++) {
                Start-Sleep -Milliseconds 250
                try { $observation = Observe-Fixture $expectedTitle } catch {
                    if ($action -eq 'SkipNext' -and $_.Exception.Message -eq 'fixture_title_mismatch') { continue }
                    throw
                }
                if ($observation.playbackStatus -eq $expectedStatus) { $observed=$true; break }
            }
            if (-not $observed) { throw [TimeoutException]::new('expected_fixture_state_not_observed') }
            Emit @{ stage=$stage; outcome='state_observed'; observation=$observation }
        }
    }
    Emit @{ stage='complete'; outcome='pass'; controlsRequested=[bool]$Control }
    exit 0
} catch {
    $exception = $_.Exception
    $denied = $false
    $kind = 'api_error'
    while ($null -ne $exception) {
        if ($exception.HResult -eq -2147024891) { $denied=$true }
        if ($exception.Message -eq 'no_current_session') { $kind='no_current_session' }
        if ($exception.Message -eq 'fixture_title_mismatch') { $kind='fixture_title_mismatch' }
        if ($exception.Message -eq 'control_rejected') { $kind='control_rejected' }
        if ($exception -is [TimeoutException]) { $kind='timeout' }
        $exception = $exception.InnerException
    }
    if ($denied) { $kind='access_denied' }
    # Exception messages may contain unrelated session data; emit only classification.
    Emit @{ stage=$stage; outcome=$kind; pass=$false }
    exit 1
}
