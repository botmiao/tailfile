# Installs Tailfile for the current Windows user with SHA-256 verification.
#
#   irm https://github.com/botmiao/tailfile/releases/latest/download/install.ps1 | iex

& {
    $ErrorActionPreference = 'Stop'

    $repo = if ($env:TAILFILE_REPO) { $env:TAILFILE_REPO.Trim() } else { 'botmiao/tailfile' }
    $reqVersion = if ($env:TAILFILE_VERSION) { $env:TAILFILE_VERSION.Trim() } else { 'latest' }
    $installDir = if ($env:TAILFILE_INSTALL_DIR) { $env:TAILFILE_INSTALL_DIR.Trim() } else { Join-Path $env:LOCALAPPDATA 'Tailfile' }
    $downloadBase = if ($env:TAILFILE_DOWNLOAD_BASE) { $env:TAILFILE_DOWNLOAD_BASE.Trim().TrimEnd('/') } else { '' }
    $skipPath = $env:TAILFILE_NO_PATH -eq '1'
    $skipShortcut = $env:TAILFILE_NO_SHORTCUT -eq '1'
    $installService = $env:TAILFILE_SERVICE -eq '1'

    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    } catch {
    }

    function Get-TargetArch {
        try {
            $osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
            switch ($osArch.ToLowerInvariant()) {
                'arm64' { return 'arm64' }
                'x64'   { return 'amd64' }
            }
        } catch {
        }
        $envArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
        switch (($envArch + '').ToUpperInvariant()) {
            'ARM64' { return 'arm64' }
            'AMD64' { return 'amd64' }
            default { throw "Unsupported Windows architecture: $envArch" }
        }
    }

    function Resolve-LatestTag([string]$repoName) {
        $latestUrl = "https://github.com/$repoName/releases/latest"
        try {
            $req = [System.Net.HttpWebRequest]::Create($latestUrl)
            $req.Method = 'HEAD'
            $req.AllowAutoRedirect = $false
            $req.UserAgent = 'tailfile-installer'
            $resp = [System.Net.HttpWebResponse]$req.GetResponse()
            try {
                $loc = $resp.Headers['Location']
                if ($loc -and $loc -match '/tag/([^/]+)$') {
                    return $Matches[1]
                }
            } finally {
                $resp.Close()
            }
        } catch {
        }

        $apiUrl = "https://api.github.com/repos/$repoName/releases/latest"
        $release = Invoke-RestMethod -Uri $apiUrl -Headers @{ 'User-Agent' = 'tailfile-installer' } -UseBasicParsing
        if (-not $release -or -not $release.tag_name) {
            throw "Failed to determine latest release tag from $repoName"
        }
        return [string]$release.tag_name
    }

    $arch = Get-TargetArch
    if ($reqVersion -eq 'latest') {
        if ($downloadBase) {
            throw 'TAILFILE_VERSION must be specified when TAILFILE_DOWNLOAD_BASE is set.'
        }
        $tag = Resolve-LatestTag $repo
    } else {
        $tag = if ($reqVersion.StartsWith('v')) { $reqVersion } else { "v$reqVersion" }
    }
    $version = $tag.TrimStart('v')
    if (-not $downloadBase) {
        $downloadBase = "https://github.com/$repo/releases/download/$tag"
    }

    $archive = "tailfile_${version}_windows_${arch}.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("tailfile-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null

    try {
        $zipPath = Join-Path $tmpDir $archive
        $sumsPath = Join-Path $tmpDir 'SHA256SUMS.txt'

        Write-Host "Downloading Tailfile $tag (windows/$arch)..."
        Invoke-WebRequest -Uri "$downloadBase/$archive" -OutFile $zipPath -UseBasicParsing
        Invoke-WebRequest -Uri "$downloadBase/SHA256SUMS.txt" -OutFile $sumsPath -UseBasicParsing

        $expectedHash = ''
        foreach ($line in (Get-Content -Path $sumsPath)) {
            if ($line -match '^\s*([0-9a-fA-F]{64})\s+\*?(.+?)\s*$') {
                if ($Matches[2] -eq $archive) {
                    $expectedHash = $Matches[1].ToLowerInvariant()
                    break
                }
            }
        }
        if (-not $expectedHash) {
            throw "Checksum entry for $archive not found in SHA256SUMS.txt"
        }

        $sha256 = [System.Security.Cryptography.SHA256]::Create()
        try {
            $stream = [System.IO.File]::OpenRead($zipPath)
            try {
                $hashBytes = $sha256.ComputeHash($stream)
            } finally {
                $stream.Dispose()
            }
        } finally {
            $sha256.Dispose()
        }
        $actualHash = ([System.BitConverter]::ToString($hashBytes) -replace '-', '').ToLowerInvariant()
        if ($actualHash -ne $expectedHash) {
            throw "SHA-256 mismatch for $archive (expected $expectedHash, got $actualHash)"
        }
        Write-Host "Verified SHA-256: $actualHash"

        $unpackDir = Join-Path $tmpDir 'unpack'
        New-Item -ItemType Directory -Force -Path $unpackDir | Out-Null
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [System.IO.Compression.ZipFile]::ExtractToDirectory($zipPath, $unpackDir)
        $srcExe = Join-Path $unpackDir 'tailfile.exe'
        if (-not (Test-Path -LiteralPath $srcExe)) {
            throw 'Archive did not contain tailfile.exe'
        }

        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        $targetExe = Join-Path $installDir 'tailfile.exe'

        $running = Get-Process -ErrorAction SilentlyContinue | Where-Object {
            $_.Path -and [string]::Equals($_.Path, $targetExe, [System.StringComparison]::OrdinalIgnoreCase)
        }
        foreach ($proc in $running) {
            Write-Host "Stopping running Tailfile (PID $($proc.Id))..."
            Stop-Process -Id $proc.Id -Force
            $proc.WaitForExit(5000) | Out-Null
        }

        $tmpExe = "$targetExe.tmp"
        Copy-Item -LiteralPath $srcExe -Destination $tmpExe -Force
        Move-Item -LiteralPath $tmpExe -Destination $targetExe -Force

        Write-Host "Installed: $targetExe"
        & $targetExe --version

        if (-not $skipPath) {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            $parts = @()
            if ($userPath) {
                $parts = $userPath -split ';' | Where-Object { $_ -and $_.Trim() -ne '' }
            }
            $hasEntry = $false
            foreach ($p in $parts) {
                if ([string]::Equals($p.Trim().TrimEnd('\'), $installDir.TrimEnd('\'), [System.StringComparison]::OrdinalIgnoreCase)) {
                    $hasEntry = $true
                    break
                }
            }
            if (-not $hasEntry) {
                $newUserPath = if ($parts.Count -gt 0) { ($parts -join ';') + ';' + $installDir } else { $installDir }
                [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
                Write-Host "Added $installDir to user PATH (open a new terminal to pick it up)."
            }
            if ($env:PATH -notlike "*$installDir*") {
                $env:PATH = "$env:PATH;$installDir"
            }
        }

        if (-not $skipShortcut) {
            try {
                $programsDir = [Environment]::GetFolderPath('Programs')
                if ($programsDir) {
                    $lnkPath = Join-Path $programsDir 'Tailfile.lnk'
                    $shell = New-Object -ComObject WScript.Shell
                    $shortcut = $shell.CreateShortcut($lnkPath)
                    $shortcut.TargetPath = $targetExe
                    $shortcut.WorkingDirectory = $installDir
                    $shortcut.Description = 'Tailfile'
                    $shortcut.Save()
                    Write-Host "Created Start Menu shortcut: $lnkPath"
                }
            } catch {
                Write-Warning "Could not create Start Menu shortcut: $_"
            }
        }

        if ($installService) {
            Write-Host 'Installing per-user background service...'
            & $targetExe service install
            & $targetExe service status
        }
    } finally {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmpDir
    }
}
