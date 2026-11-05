$ErrorActionPreference = 'Stop'

function Invoke-ProjectToolchain {
    param([Parameter(Mandatory)][scriptblock]$Action)
    $projectRoot = Split-Path -Parent $PSScriptRoot
    $names = @('PATH', 'GOPATH', 'GOMODCACHE', 'GOCACHE', 'GOBIN', 'CGO_ENABLED')
    $previous = @{}
    foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
    Push-Location $projectRoot
    try {
        if (Test-Path -LiteralPath '.tools/go/bin/go.exe') {
            $env:PATH = "$(Join-Path $projectRoot '.tools/go/bin');$env:PATH"
        }
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw 'Go is missing. Run scripts/bootstrap.ps1 first.'
        }
        $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
        $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
        $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
        $env:GOBIN = Join-Path $projectRoot '.tools/bin'
        $env:CGO_ENABLED = '0'
        & $Action
    } finally {
        Pop-Location
        foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
    }
}

function Assert-ExitCode([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed (exit $LASTEXITCODE)" }
}

# frontend/package.json owns the release version; every gate reads it here so a
# bump cannot leave one script checking a stale literal.
function Get-ProjectVersion {
    $root = Split-Path -Parent $PSScriptRoot
    return (Get-Content -Raw -LiteralPath (Join-Path $root 'frontend/package.json') | ConvertFrom-Json).version
}

# The packaged README states the release on its first line, so it is a payload
# field like the version resource. The check lives here rather than in build.ps1
# because `package-windows.ps1 -SkipBuild` ships that file without building it:
# without this, a stale README could be zipped next to a newer executable.
function Assert-PayloadVersionDocuments {
    param([Parameter(Mandatory)][string]$Version)
    $root = Split-Path -Parent $PSScriptRoot
    $firstLine = (Get-Content -TotalCount 1 -LiteralPath (Join-Path $root 'installer/README.txt'))
    # Extract the whole dotted number: a substring test would accept 0.5.40 or 0.5.4.1.
    $declared = [regex]::Match($firstLine, '\d+(?:\.\d+)+').Value
    if ($declared -ne $Version) {
        throw "installer/README.txt line 1 does not state version $Version -- found '$firstLine'"
    }
}

# The icon sizes the build asks `wails3 generate icons` for. Build and acceptance
# scripts both read this so the embedded entry count is a checked invariant rather
# than a threshold.
function Get-IconSizePlan {
    return @(256, 128, 64, 48, 40, 32, 24, 16)
}

function Get-WindowsResourceInventory {
    param([Parameter(Mandatory)][string]$ExePath)
    if (-not ('OtoneiWin32Resources' -as [type])) {
        Add-Type -TypeDefinition @'
using System;using System.Collections.Generic;using System.Runtime.InteropServices;
public static class OtoneiWin32Resources {
 public delegate bool EnumResNameProc(IntPtr h,IntPtr type,IntPtr name,IntPtr p);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr LoadLibraryEx(string path,IntPtr h,uint flags);
 [DllImport("kernel32.dll")] public static extern bool FreeLibrary(IntPtr m);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode)] public static extern bool EnumResourceNames(IntPtr m,IntPtr type,EnumResNameProc cb,IntPtr p);
 public static List<string> Names = new List<string>();
 public static bool Collect(IntPtr h,IntPtr type,IntPtr name,IntPtr p){
  Names.Add((name.ToInt64() >> 16)==0 ? ((int)(short)name.ToInt64()).ToString() : name.ToInt64().ToString());
  return true; }
 // RT_ICON, RT_GROUP_ICON, RT_VERSION, RT_MANIFEST. A type the image does not
 // carry makes EnumResourceNames fail with ERROR_NOT_FOUND; report it as zero
 // entries so the caller can name the missing kind.
 public static Dictionary<string,int> Count(IntPtr module,int[] types){
  var result = new Dictionary<string,int>();
  string[] labels = {"icon","groupIcon","version","manifest"};
  for(int i=0;i<types.Length;i++){
   Names = new List<string>();
   bool ok = EnumResourceNames(module,new IntPtr(types[i]),Collect,IntPtr.Zero);
   result[labels[i]] = ok ? Names.Count : 0;
  }
  return result;
 }
}
'@
    }
    $handle = [OtoneiWin32Resources]::LoadLibraryEx($ExePath, [IntPtr]::Zero, 0x2) # LOAD_LIBRARY_AS_DATAFILE
    if ($handle -eq [IntPtr]::Zero) { throw "Cannot open '$ExePath' as a data file." }
    try { return [OtoneiWin32Resources]::Count($handle, @(3, 14, 16, 24)) } finally { [OtoneiWin32Resources]::FreeLibrary($handle) | Out-Null }
}

# The resource kinds the release promises. ExtractAssociatedIcon is not proof of
# anything: for an image without resources the shell hands back its own generic
# icon, so presence is read from the PE resource directory instead.
function Assert-WindowsResources {
    param([Parameter(Mandatory)][string]$ExePath, [Parameter(Mandatory)][int]$ExpectedIconEntries)
    $inventory = Get-WindowsResourceInventory -ExePath $ExePath
    foreach ($kind in @('icon', 'groupIcon', 'version', 'manifest')) {
        if ($inventory[$kind] -lt 1) { throw "'$ExePath' carries no RT_${kind} resource entry." }
    }
    if ($inventory['icon'] -ne $ExpectedIconEntries) {
        throw "'$ExePath' has $($inventory['icon']) RT_ICON entries, expected $ExpectedIconEntries."
    }
    return $inventory
}

function Assert-ExecutableVersion {
    param([Parameter(Mandatory)][string]$ExePath, [Parameter(Mandatory)][string]$Version)
    $info = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($ExePath)
    # Exact equality: a prefix test would accept 0.5.40 for a 0.5.4 release.
    if ($info.ProductName -ne 'OTONEIGO' -or $info.ProductVersion -ne $Version -or $info.FileVersion -ne $Version) {
        throw "Executable version resource mismatch in '$ExePath': ProductName='$($info.ProductName)' ProductVersion='$($info.ProductVersion)' FileVersion='$($info.FileVersion)', expected OTONEIGO $Version."
    }
    return $info
}
