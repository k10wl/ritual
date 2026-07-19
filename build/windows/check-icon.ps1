# Fails the build if the just-linked exe has no embedded icon resource.
#
# ExtractAssociatedIcon (the naive check) always returns something - Explorer
# synthesizes a generic default icon for any exe with zero resources of its
# own, so a null/non-null check can't tell "branded icon linked" from
# "generate:syso silently produced nothing and go build linked no resource
# at all". ExtractIconEx's icon COUNT is the actual resource-table content;
# 0 means no icon resource is embedded, whatever Explorer shows on top of it.
# See design-log/050 (Windows-icon findings).
#
# Plain ASCII only: PowerShell 5.1 reads a BOM-less script under the system
# ANSI codepage, and a stray multi-byte UTF-8 character (em-dash, section
# mark) gets misdecoded and corrupts parsing far from the actual character -
# manifests as bogus "missing string terminator" errors on unrelated lines.
param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath
)

$resolved = Resolve-Path $ExePath -ErrorAction Stop

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public class RitualIconCheck {
    [DllImport("shell32.dll", CharSet = CharSet.Auto)]
    public static extern int ExtractIconEx(string szFileName, int nIconIndex, IntPtr[] phiconLarge, IntPtr[] phiconSmall, int nIcons);
}
'@

$count = [RitualIconCheck]::ExtractIconEx($resolved.Path, -1, $null, $null, 0)

if ($count -lt 1) {
    Write-Error "No icon resource embedded in $resolved (ExtractIconEx returned $count). generate:syso likely failed, or its .syso wasn't linked by go build - see design-log/050."
    exit 1
}

Write-Host "icon check OK: $resolved carries $count icon resource(s)"
