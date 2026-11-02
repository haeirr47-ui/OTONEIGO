# Inspect only the isolated acceptance process's own HWNDs. No global changes.
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
$testProcessId=[int](Get-Content (Join-Path $projectRoot 'output/native-desktop-05/process-id.txt'))
$testProcess=Get-Process -Id $testProcessId -ErrorAction Stop
if($testProcess.Path -ne (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe')){throw 'Unexpected test process identity'}
Add-Type @'
using System;using System.Text;using System.Runtime.InteropServices;
public static class Desktop05Window {
 public delegate bool Callback(IntPtr h,IntPtr p);
 [DllImport("user32.dll")]public static extern bool EnumWindows(Callback c,IntPtr p);
 [DllImport("user32.dll")]public static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)]public static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)]public static extern int GetClassName(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",EntryPoint="GetWindowLongW")]public static extern int GetWindowLong(IntPtr h,int n);
 [DllImport("user32.dll")]public static extern uint GetDpiForWindow(IntPtr h);
}
'@
function Read-TestWindows {
 $found=[Collections.Generic.List[object]]::new()
 [Desktop05Window]::EnumWindows({param($handle,$unused)
  $owner=0;[Desktop05Window]::GetWindowThreadProcessId($handle,[ref]$owner)|Out-Null
  if($owner -eq $testProcessId){
   $title=[Text.StringBuilder]::new(512);$class=[Text.StringBuilder]::new(128)
   [Desktop05Window]::GetWindowText($handle,$title,512)|Out-Null;[Desktop05Window]::GetClassName($handle,$class,128)|Out-Null
   if($class.ToString() -eq 'WailsWebviewWindow' -and ($title.ToString() -eq '桌面歌词' -or $title.ToString() -eq 'OTONEIGO迷你播放器' -or $title.ToString() -eq 'OTONEIGO' -or $title.ToString().EndsWith(' — OTONEIGO'))){
    $role=if($title.ToString() -eq '桌面歌词'){'lyrics'}elseif($title.ToString() -eq 'OTONEIGO迷你播放器'){'mini'}else{'main'}
    $style=[Desktop05Window]::GetWindowLong($handle,-20)
    $found.Add([pscustomobject]@{role=$role;clickThrough=($style -band 0x20) -ne 0;topmost=($style -band 0x8) -ne 0;dpi=[Desktop05Window]::GetDpiForWindow($handle)})
   }
  };return $true
 },[IntPtr]::Zero)|Out-Null
 return @($found)
}
$toggle=@'
import {nativeHarness,until} from "./scripts/native-harness.mjs";
const h=await nativeHarness();const s=await h.call("GetDesktopSnapshot");
await h.call("DispatchDesktopCommand",{epoch:s.epoch,command:"toggle-lyrics-lock",songId:s.songId,value:0});
await until(async()=>(await h.call("GetDesktopNativeState")).lyricsLocked!==s.options.lyricsLocked);
await h.browser.close();
'@
$before=Read-TestWindows
if($before.Count -ne 3 -or ($before|Where-Object role -eq 'lyrics').clickThrough){throw 'Expected three unlocked native windows'}
$locked=$false
try{
 & node --input-type=module -e $toggle
 if($LASTEXITCODE -ne 0){throw 'Native lock command failed'}
 $locked=$true;$during=Read-TestWindows
 if(-not ($during|Where-Object role -eq 'lyrics').clickThrough){throw 'WS_EX_TRANSPARENT missing on locked lyrics'}
 if(-not ($during|Where-Object role -eq 'lyrics').topmost -or -not ($during|Where-Object role -eq 'mini').topmost){throw 'Always-on-top native style missing'}
}finally{
 if($locked){& node --input-type=module -e $toggle;if($LASTEXITCODE -ne 0){throw 'Native unlock command failed'}}
}
$after=Read-TestWindows
if(($after|Where-Object role -eq 'lyrics').clickThrough){throw 'Lyrics remained click-through after unlocking'}
$report=[ordered]@{nativeWindows=$true;before=$before;locked=$during;unlocked=$after;pass=$true;physicalMultiMonitorChange=$false}
$report|ConvertTo-Json -Depth 5|Set-Content (Join-Path $projectRoot 'output/playwright/native-desktop05-windowstyle-report.json') -Encoding utf8
$report|ConvertTo-Json -Depth 5
