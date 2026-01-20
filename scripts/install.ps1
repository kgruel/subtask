Param(
  [string]$BinDir = "$env:LOCALAPPDATA\\Subtask\\bin",
  [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "zippoxer/subtask"

function Normalize-Tag([string]$Tag) {
  if ($Tag -eq "") { return "" }
  if ($Tag.StartsWith("v")) { return $Tag }
  return "v$Tag"
}

$arch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }

$goArch = switch ($arch) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "Unsupported architecture: $arch" }
}

$tag = Normalize-Tag $Version
$apiUrl = if ($tag -eq "") {
  "https://api.github.com/repos/$Repo/releases/latest"
} else {
  "https://api.github.com/repos/$Repo/releases/tags/$tag"
}

$rel = Invoke-RestMethod -Headers @{ "Accept" = "application/vnd.github+json"; "User-Agent" = "subtask-install" } -Uri $apiUrl
$tagName = $rel.tag_name
if (-not $tagName) { throw "Failed to determine release tag from GitHub API response." }

$asset = $rel.assets | Where-Object { $_.name -match "_windows_${goArch}\.zip$" } | Select-Object -First 1
if (-not $asset) { throw "Failed to find a release asset for windows/$goArch in $tagName." }

$checksums = $rel.assets | Where-Object { $_.name -eq "checksums.txt" } | Select-Object -First 1
if (-not $checksums) { throw "Failed to find checksums.txt in $tagName." }

$tmp = Join-Path $env:TEMP ("subtask-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
  $zipPath = Join-Path $tmp $asset.name
  $checksumsPath = Join-Path $tmp "checksums.txt"

  Write-Host "Downloading subtask $tagName (windows/$goArch)..."
  Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath | Out-Null
  Invoke-WebRequest -Uri $checksums.browser_download_url -OutFile $checksumsPath | Out-Null

  $line = Get-Content $checksumsPath | Where-Object { $_ -match "\s+$([Regex]::Escape($asset.name))$" } | Select-Object -First 1
  if (-not $line) { throw "Failed to find checksum for $($asset.name) in checksums.txt." }
  $expected = $line.Split()[0]

  $actual = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) {
    throw "Checksum mismatch for $($asset.name). Expected $expected, got $actual."
  }

  $extractDir = Join-Path $tmp "extract"
  Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

  $bin = Get-ChildItem -Path $extractDir -Recurse -Filter "subtask.exe" | Select-Object -First 1
  if (-not $bin) { throw "Failed to find subtask.exe in archive." }

  New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
  Copy-Item -Force -Path $bin.FullName -Destination (Join-Path $BinDir "subtask.exe")

  # Add to user PATH if needed.
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $pathParts = @()
  if ($userPath) { $pathParts = $userPath.Split(';') }
  if ($pathParts -notcontains $BinDir) {
    $newUserPath = if ($userPath) { "$userPath;$BinDir" } else { $BinDir }
    [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
  }
  if ($env:Path -notmatch [Regex]::Escape($BinDir)) {
    $env:Path = "$env:Path;$BinDir"
  }

  Write-Host "Installed subtask to $(Join-Path $BinDir 'subtask.exe')"
  Write-Host "If this is your first install, open a new terminal for PATH changes to take effect."
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
