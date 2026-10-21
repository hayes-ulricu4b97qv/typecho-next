param(
    [ValidateSet('dev', 'web-build', 'api-run', 'build', 'package', 'fmt-check', 'clean')]
    [string]$Task = 'build',
    [string]$Version = '',
    [string]$OutputDirectory = ''
)

$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') {
    throw 'Typecho Next supports Windows only.'
}

function Invoke-CommandChecked {
    param([string]$Executable, [string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable exited with code $LASTEXITCODE"
    }
}

function Test-PackagedRuntime {
    param([string]$PackageRoot, [string]$SmokeRoot)

    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try {
        $listener.Start()
        $port = $listener.LocalEndpoint.Port
    } finally {
        $listener.Stop()
    }

    $dataDirectory = Join-Path $SmokeRoot 'data'
    $mediaDirectory = Join-Path $SmokeRoot 'media'
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $env:ComSpec
    $startInfo.Arguments = '/d /c start.cmd'
    $startInfo.WorkingDirectory = $PackageRoot
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.EnvironmentVariables['APP_PORT'] = [string]$port
    $startInfo.EnvironmentVariables['APP_LISTEN_HOST'] = '127.0.0.1'
    $startInfo.EnvironmentVariables['APP_DATA_DIR'] = $dataDirectory
    $startInfo.EnvironmentVariables['APP_MEDIA_DIR'] = $mediaDirectory
    $startInfo.EnvironmentVariables['PATH'] = Join-Path $env:SystemRoot 'System32'

    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    $started = $false
    $stdoutTask = $null
    $stderrTask = $null
    $restoredProcess = $null
    $restoreStarted = $false
    $restoreStdoutTask = $null
    $restoreStderrTask = $null
    try {
        if (-not $process.Start()) {
            throw 'Could not start the packaged launcher.'
        }
        $started = $true
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        Add-Type -AssemblyName System.Net.Http
        $httpHandler = [System.Net.Http.HttpClientHandler]::new()
        $httpHandler.UseProxy = $false
        $httpHandler.CookieContainer = [System.Net.CookieContainer]::new()
        $http = [System.Net.Http.HttpClient]::new($httpHandler)
        $http.Timeout = [TimeSpan]::FromSeconds(2)
        try {
            $baseURL = "http://127.0.0.1:$port"
            $deadline = [DateTime]::UtcNow.AddSeconds(20)
            $healthy = $false
            $lastRequestError = ''
            while ([DateTime]::UtcNow -lt $deadline) {
                if ($process.HasExited) {
                    $details = @()
                    if ($stdoutTask.IsCompleted) { $details += $stdoutTask.Result.Trim() }
                    if ($stderrTask.IsCompleted) { $details += $stderrTask.Result.Trim() }
                    throw "Packaged launcher exited with code $($process.ExitCode). $($details -join ' ')"
                }
                try {
                    $response = $http.GetAsync("$baseURL/healthz").GetAwaiter().GetResult()
                    try {
                        if ($response.StatusCode -eq [System.Net.HttpStatusCode]::OK) {
                            $healthy = $true
                            break
                        }
                    } finally {
                        $response.Dispose()
                    }
                } catch [System.Net.Http.HttpRequestException] {
                    $lastRequestError = $_.Exception.Message
                } catch [System.Threading.Tasks.TaskCanceledException] {
                    $lastRequestError = 'The health check request timed out.'
                }
                Start-Sleep -Milliseconds 100
            }
            if (-not $healthy) {
                throw "Packaged launcher did not become healthy within 20 seconds. $lastRequestError"
            }

            $readyResponse = $http.GetAsync("$baseURL/readyz").GetAwaiter().GetResult()
            try {
                if ($readyResponse.StatusCode -ne [System.Net.HttpStatusCode]::ServiceUnavailable) {
                    throw "Uninitialized packaged app returned /readyz status $([int]$readyResponse.StatusCode), expected 503."
                }
            } finally {
                $readyResponse.Dispose()
            }

            $settingResponse = $http.GetAsync("$baseURL/setting").GetAwaiter().GetResult()
            try {
                if ($settingResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK) {
                    throw "Packaged setup page returned status $([int]$settingResponse.StatusCode), expected 200."
                }
            } finally {
                $settingResponse.Dispose()
            }

            $statusResponse = $http.GetAsync("$baseURL/api/v1/system/status").GetAwaiter().GetResult()
            try {
                if ($statusResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK) {
                    throw "Packaged system status returned $([int]$statusResponse.StatusCode), expected 200."
                }
            } finally {
                $statusResponse.Dispose()
            }

            $ownerPassword = 'smoke-password-123'
            $setupPayload = @{
                siteName = 'Release Smoke'
                siteUrl = $baseURL
                adminUsername = 'smoke-owner'
                adminPassword = $ownerPassword
                adminEmail = 'owner@example.com'
                timezone = 'UTC'
            } | ConvertTo-Json -Compress
            $setupContent = [System.Net.Http.StringContent]::new($setupPayload, [Text.Encoding]::UTF8, 'application/json')
            try {
                $setupResponse = $http.PostAsync("$baseURL/api/v1/setup", $setupContent).GetAwaiter().GetResult()
            } finally {
                $setupContent.Dispose()
            }
            try {
                $setupBody = $setupResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($setupResponse.StatusCode -ne [System.Net.HttpStatusCode]::Created) {
                    throw "Packaged setup returned $([int]$setupResponse.StatusCode): $setupBody"
                }
                if (-not (ConvertFrom-Json -InputObject $setupBody).initialized) {
                    throw 'Packaged setup response did not confirm initialization.'
                }
            } finally {
                $setupResponse.Dispose()
            }

            $readyResponse = $http.GetAsync("$baseURL/readyz").GetAwaiter().GetResult()
            try {
                if ($readyResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK) {
                    throw "Initialized packaged app returned /readyz status $([int]$readyResponse.StatusCode), expected 200."
                }
            } finally {
                $readyResponse.Dispose()
            }

            $loginPayload = @{ username = 'smoke-owner'; password = $ownerPassword } | ConvertTo-Json -Compress
            $loginContent = [System.Net.Http.StringContent]::new($loginPayload, [Text.Encoding]::UTF8, 'application/json')
            try {
                $loginResponse = $http.PostAsync("$baseURL/api/v1/auth/login", $loginContent).GetAwaiter().GetResult()
            } finally {
                $loginContent.Dispose()
            }
            try {
                $loginBody = $loginResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($loginResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK) {
                    throw "Packaged owner login returned $([int]$loginResponse.StatusCode): $loginBody"
                }
                if ((ConvertFrom-Json -InputObject $loginBody).user.role -ne 'owner') {
                    throw 'Packaged owner login returned an unexpected role.'
                }
            } finally {
                $loginResponse.Dispose()
            }

            $identityResponse = $http.GetAsync("$baseURL/api/v1/auth/me").GetAwaiter().GetResult()
            try {
                $identityBody = $identityResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($identityResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK -or (ConvertFrom-Json -InputObject $identityBody).user.username -ne 'smoke-owner') {
                    throw "Packaged owner session could not be read back: $identityBody"
                }
            } finally {
                $identityResponse.Dispose()
            }

            $csrfCookie = @($httpHandler.CookieContainer.GetCookies([Uri]$baseURL) | Where-Object { $_.Name -eq 'app_csrf' }) | Select-Object -First 1
            if ($null -eq $csrfCookie) {
                throw 'Packaged app did not issue a CSRF cookie.'
            }
            $gifBytes = [Convert]::FromBase64String('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7')
            $uploadForm = [System.Net.Http.MultipartFormDataContent]::new()
            $uploadFile = [System.Net.Http.ByteArrayContent]::new($gifBytes)
            $uploadFile.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse('image/gif')
            $uploadForm.Add($uploadFile, 'file', 'release-smoke.gif')
            $uploadRequest = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$baseURL/api/v1/media")
            $uploadRequest.Headers.Add('X-CSRF-Token', $csrfCookie.Value)
            $uploadRequest.Content = $uploadForm
            try {
                $uploadResponse = $http.SendAsync($uploadRequest).GetAwaiter().GetResult()
            } finally {
                $uploadRequest.Dispose()
            }
            try {
                $uploadResponseBody = $uploadResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($uploadResponse.StatusCode -ne [System.Net.HttpStatusCode]::Created) {
                    throw "Packaged media upload returned $([int]$uploadResponse.StatusCode): $uploadResponseBody"
                }
                $uploadedMedia = ConvertFrom-Json -InputObject $uploadResponseBody
                if ([string]::IsNullOrWhiteSpace($uploadedMedia.id) -or $uploadedMedia.contentType -ne 'image/gif') {
                    throw "Packaged media upload returned invalid metadata: $uploadResponseBody"
                }
            } finally {
                $uploadResponse.Dispose()
            }

            $postBody = "Release backup smoke`n`n![pixel]($($uploadedMedia.url))"
            $postPayload = @{
                title = 'Release Backup Smoke'
                slug = 'release-backup-smoke'
                content = $postBody
                status = 'published'
            } | ConvertTo-Json -Compress
            $postRequest = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$baseURL/api/v1/content/posts")
            $postRequest.Headers.Add('X-CSRF-Token', $csrfCookie.Value)
            $postRequest.Content = [System.Net.Http.StringContent]::new($postPayload, [Text.Encoding]::UTF8, 'application/json')
            try {
                $postResponse = $http.SendAsync($postRequest).GetAwaiter().GetResult()
            } finally {
                $postRequest.Dispose()
            }
            try {
                $postResponseBody = $postResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($postResponse.StatusCode -ne [System.Net.HttpStatusCode]::Created) {
                    throw "Packaged post creation returned $([int]$postResponse.StatusCode): $postResponseBody"
                }
            } finally {
                $postResponse.Dispose()
            }
            $publicPost = $http.GetAsync("$baseURL/api/v1/content/posts/release-backup-smoke").GetAwaiter().GetResult()
            try {
                $publicPostBody = $publicPost.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($publicPost.StatusCode -ne [System.Net.HttpStatusCode]::OK -or (ConvertFrom-Json -InputObject $publicPostBody).content -ne $postBody) {
                    throw "Packaged public post returned unexpected content: $publicPostBody"
                }
            } finally {
                $publicPost.Dispose()
            }

            $sourceMediaFiles = @(Get-ChildItem -LiteralPath $mediaDirectory -File)
            if ($sourceMediaFiles.Count -ne 1) {
                throw "Packaged upload created $($sourceMediaFiles.Count) media files, expected one."
            }
            $sourceMediaPath = $sourceMediaFiles[0].FullName
            $heldMediaPath = Join-Path $SmokeRoot 'held-release-smoke.gif'
            Move-Item -LiteralPath $sourceMediaPath -Destination $heldMediaPath
            try {
                $failedBackupResponse = $http.GetAsync("$baseURL/api/v1/backup").GetAwaiter().GetResult()
                try {
                    $failedBackupBody = $failedBackupResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                    if ($failedBackupResponse.StatusCode -ne [System.Net.HttpStatusCode]::InternalServerError -or (ConvertFrom-Json -InputObject $failedBackupBody).error.code -ne 'backup_failed') {
                        throw "Missing-media backup returned $([int]$failedBackupResponse.StatusCode): $failedBackupBody"
                    }
                } finally {
                    $failedBackupResponse.Dispose()
                }
            } finally {
                if (Test-Path -LiteralPath $heldMediaPath -PathType Leaf) {
                    [IO.File]::Move($heldMediaPath, $sourceMediaPath)
                }
            }

            $backupResponse = $http.GetAsync("$baseURL/api/v1/backup").GetAwaiter().GetResult()
            try {
                if ($backupResponse.StatusCode -ne [System.Net.HttpStatusCode]::OK -or $backupResponse.Content.Headers.ContentType.MediaType -ne 'application/zip') {
                    $backupError = $backupResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                    throw "Packaged backup returned $([int]$backupResponse.StatusCode): $backupError"
                }
                $backupBytes = $backupResponse.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
            } finally {
                $backupResponse.Dispose()
            }
            $backupPath = Join-Path $SmokeRoot 'backup.zip'
            [IO.File]::WriteAllBytes($backupPath, $backupBytes)
            $recoveryDirectory = Join-Path $SmokeRoot 'recovery'
            Expand-Archive -LiteralPath $backupPath -DestinationPath $recoveryDirectory
            if (-not (Test-Path -LiteralPath (Join-Path $recoveryDirectory 'app.db') -PathType Leaf)) {
                throw 'Packaged backup does not contain app.db.'
            }
            $recoveredMediaDirectory = Join-Path $recoveryDirectory 'media'
            if (-not (Test-Path -LiteralPath $recoveredMediaDirectory -PathType Container)) {
                throw 'Packaged backup does not contain the uploaded media directory.'
            }
            $recoveredMediaFiles = @(Get-ChildItem -LiteralPath $recoveredMediaDirectory -File)
            if ($recoveredMediaFiles.Count -ne 1 -or [Convert]::ToBase64String([IO.File]::ReadAllBytes($recoveredMediaFiles[0].FullName)) -ne [Convert]::ToBase64String($gifBytes)) {
                throw 'Packaged backup does not contain the exact uploaded media file.'
            }

            $restoreListener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
            try {
                $restoreListener.Start()
                $restorePort = $restoreListener.LocalEndpoint.Port
            } finally {
                $restoreListener.Stop()
            }
            $restoreMediaDirectory = Join-Path $recoveryDirectory 'media'
            $restoreStartInfo = [Diagnostics.ProcessStartInfo]::new()
            $restoreStartInfo.FileName = $env:ComSpec
            $restoreStartInfo.Arguments = '/d /c start.cmd'
            $restoreStartInfo.WorkingDirectory = $PackageRoot
            $restoreStartInfo.UseShellExecute = $false
            $restoreStartInfo.CreateNoWindow = $true
            $restoreStartInfo.RedirectStandardOutput = $true
            $restoreStartInfo.RedirectStandardError = $true
            $restoreStartInfo.EnvironmentVariables['APP_PORT'] = [string]$restorePort
            $restoreStartInfo.EnvironmentVariables['APP_LISTEN_HOST'] = '127.0.0.1'
            $restoreStartInfo.EnvironmentVariables['APP_DATA_DIR'] = $recoveryDirectory
            $restoreStartInfo.EnvironmentVariables['APP_MEDIA_DIR'] = $restoreMediaDirectory
            $restoreStartInfo.EnvironmentVariables['PATH'] = Join-Path $env:SystemRoot 'System32'
            $restoredProcess = [Diagnostics.Process]::new()
            $restoredProcess.StartInfo = $restoreStartInfo
            if (-not $restoredProcess.Start()) {
                throw 'Could not start the packaged app from the restored backup.'
            }
            $restoreStarted = $true
            $restoreStdoutTask = $restoredProcess.StandardOutput.ReadToEndAsync()
            $restoreStderrTask = $restoredProcess.StandardError.ReadToEndAsync()
            $restoreBaseURL = "http://127.0.0.1:$restorePort"
            $restoreDeadline = [DateTime]::UtcNow.AddSeconds(20)
            $restoreHealthy = $false
            while ([DateTime]::UtcNow -lt $restoreDeadline) {
                if ($restoredProcess.HasExited) {
                    $restoreDetails = @()
                    if ($restoreStdoutTask.IsCompleted) { $restoreDetails += $restoreStdoutTask.Result.Trim() }
                    if ($restoreStderrTask.IsCompleted) { $restoreDetails += $restoreStderrTask.Result.Trim() }
                    throw "Packaged app exited after restore with code $($restoredProcess.ExitCode). $($restoreDetails -join ' ')"
                }
                try {
                    $restoreHealth = $http.GetAsync("$restoreBaseURL/healthz").GetAwaiter().GetResult()
                    try {
                        if ($restoreHealth.StatusCode -eq [System.Net.HttpStatusCode]::OK) {
                            $restoreHealthy = $true
                            break
                        }
                    } finally {
                        $restoreHealth.Dispose()
                    }
                } catch [System.Net.Http.HttpRequestException] {
                    $lastRequestError = $_.Exception.Message
                } catch [System.Threading.Tasks.TaskCanceledException] {
                    $lastRequestError = 'The restored health check request timed out.'
                }
                Start-Sleep -Milliseconds 100
            }
            if (-not $restoreHealthy) {
                throw "Restored packaged app did not become healthy within 20 seconds. $lastRequestError"
            }
            $restoredReady = $http.GetAsync("$restoreBaseURL/readyz").GetAwaiter().GetResult()
            try {
                if ($restoredReady.StatusCode -ne [System.Net.HttpStatusCode]::OK) {
                    throw "Restored packaged app returned /readyz status $([int]$restoredReady.StatusCode), expected 200."
                }
            } finally {
                $restoredReady.Dispose()
            }
            $restoredIdentity = $http.GetAsync("$restoreBaseURL/api/v1/auth/me").GetAwaiter().GetResult()
            try {
                $restoredIdentityBody = $restoredIdentity.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($restoredIdentity.StatusCode -ne [System.Net.HttpStatusCode]::OK -or (ConvertFrom-Json -InputObject $restoredIdentityBody).user.username -ne 'smoke-owner') {
                    throw "Restored owner session could not be read back: $restoredIdentityBody"
                }
            } finally {
                $restoredIdentity.Dispose()
            }
            $restoredPost = $http.GetAsync("$restoreBaseURL/api/v1/content/posts/release-backup-smoke").GetAwaiter().GetResult()
            try {
                $restoredPostBody = $restoredPost.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($restoredPost.StatusCode -ne [System.Net.HttpStatusCode]::OK -or (ConvertFrom-Json -InputObject $restoredPostBody).content -ne $postBody) {
                    throw "Restored post could not be read back: $restoredPostBody"
                }
            } finally {
                $restoredPost.Dispose()
            }
            $restoredImage = $http.GetAsync("$restoreBaseURL$($uploadedMedia.url)").GetAwaiter().GetResult()
            try {
                $restoredImageBytes = $restoredImage.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
                if ($restoredImage.StatusCode -ne [System.Net.HttpStatusCode]::OK -or [Convert]::ToBase64String($restoredImageBytes) -ne [Convert]::ToBase64String($gifBytes)) {
                    throw 'Restored media endpoint did not return the uploaded image.'
                }
            } finally {
                $restoredImage.Dispose()
            }

            $databasePath = Join-Path $dataDirectory 'app.db'
            if (-not (Test-Path -LiteralPath $databasePath -PathType Leaf) -or (Get-Item -LiteralPath $databasePath).Length -lt 1) {
                throw 'Packaged app did not create app.db in APP_DATA_DIR.'
            }
            if (-not (Test-Path -LiteralPath $mediaDirectory -PathType Container)) {
                throw 'Packaged launcher did not create APP_MEDIA_DIR.'
            }
        } finally {
            $http.Dispose()
            $httpHandler.Dispose()
        }
    } finally {
        if ($null -ne $restoredProcess) {
            if ($restoreStarted -and -not $restoredProcess.HasExited) {
                & (Join-Path $env:SystemRoot 'System32\taskkill.exe') /PID $restoredProcess.Id /T /F 2>$null | Out-Null
                if (-not $restoredProcess.HasExited) {
                    try { $restoredProcess.Kill() } catch [InvalidOperationException] { }
                }
            }
            if ($restoreStarted -and -not $restoredProcess.WaitForExit(10000)) {
                throw 'Restored packaged app did not stop within 10 seconds.'
            }
            $restoredProcess.Dispose()
        }
        if ($started) {
            if (-not $process.HasExited) {
                & (Join-Path $env:SystemRoot 'System32\taskkill.exe') /PID $process.Id /T /F 2>$null | Out-Null
                if (-not $process.HasExited) {
                    try { $process.Kill() } catch [InvalidOperationException] { }
                }
            }
            if (-not $process.WaitForExit(10000)) {
                throw 'Packaged launcher did not stop within 10 seconds.'
            }
        }
        $process.Dispose()
    }
}

function Build-Web {
    Push-Location (Join-Path $PSScriptRoot 'web')
    try {
        Invoke-CommandChecked 'npm.cmd' @('ci')
        Invoke-CommandChecked 'npm.cmd' @('run', 'build')
    } finally {
        Pop-Location
    }
}

function Package-Release {
    if ($Version -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$') {
        throw 'Package version must be a semantic version such as 1.2.3 or 1.2.3-rc.1.'
    }

    if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
        $outputRoot = Join-Path $PSScriptRoot 'release-artifacts'
    } elseif ([IO.Path]::IsPathRooted($OutputDirectory)) {
        $outputRoot = [IO.Path]::GetFullPath($OutputDirectory)
    } else {
        $outputRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot $OutputDirectory))
    }

    $archiveName = "typecho-next-v$Version-windows-amd64.zip"
    $archivePath = Join-Path $outputRoot $archiveName
    if (Test-Path -LiteralPath $archivePath) {
        throw "Release package already exists: $archivePath"
    }
    $candidateArchivePath = Join-Path $outputRoot ".typecho-next-v$Version-$([guid]::NewGuid().ToString('N')).tmp.zip"

    $stagingRoot = Join-Path ([IO.Path]::GetTempPath()) "typecho-next-package-$([guid]::NewGuid().ToString('N'))"
    $packageRoot = Join-Path $stagingRoot "typecho-next-v$Version-windows-amd64"
    $previousEnvironment = @{}
    foreach ($name in @('CGO_ENABLED', 'GOOS', 'GOARCH')) {
        $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    }

    try {
        New-Item -ItemType Directory -Force -Path $packageRoot | Out-Null
        Build-Web

        $env:CGO_ENABLED = '1'
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $executablePath = Join-Path $packageRoot 'typecho-next.exe'
        $linkerFlags = "-X github.com/hayes-ulricu4b97qv/tyoechonext/internal/config.BuildVersion=$Version -extldflags=-static"
        Invoke-CommandChecked 'go' @('build', '-trimpath', '-ldflags', $linkerFlags, '-o', $executablePath, './cmd/server')

        $webDist = Join-Path $packageRoot 'web\dist'
        New-Item -ItemType Directory -Force -Path $webDist | Out-Null
        Copy-Item -Path (Join-Path $PSScriptRoot 'web\dist\*') -Destination $webDist -Recurse -Force
        New-Item -ItemType Directory -Force -Path (Join-Path $packageRoot 'docs') | Out-Null
        foreach ($relativePath in @('start.cmd', 'CONTRIBUTING.md', 'LICENSE.txt', 'SECURITY.md', '.env.example', 'docs\architecture.md', 'docs\e2e-testing.md', 'docs\operations.md', 'docs\productization.md')) {
            $sourcePath = Join-Path $PSScriptRoot $relativePath
            $destinationPath = Join-Path $packageRoot $relativePath
            $destinationDirectory = Split-Path -Parent $destinationPath
            New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null
            Copy-Item -LiteralPath $sourcePath -Destination $destinationPath -Force
        }
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'docs\release-readme.md') -Destination (Join-Path $packageRoot 'README.md') -Force
        [IO.File]::WriteAllText((Join-Path $packageRoot 'VERSION'), "$Version`r`n", [Text.UTF8Encoding]::new($false))
        $buildCommit = $env:RELEASE_COMMIT
        if ([string]::IsNullOrWhiteSpace($buildCommit)) {
            $buildCommit = 'local'
        }
        [IO.File]::WriteAllText((Join-Path $packageRoot 'BUILD.txt'), "version=$Version`ncommit=$buildCommit`n", [Text.UTF8Encoding]::new($false))

        $requiredPackageFiles = @('typecho-next.exe', 'start.cmd', 'README.md', 'LICENSE.txt', 'SECURITY.md', '.env.example', 'VERSION', 'BUILD.txt', 'docs\architecture.md', 'docs\e2e-testing.md', 'docs\operations.md', 'docs\productization.md', 'web\dist\index.html')
        foreach ($relativePath in $requiredPackageFiles) {
            if (-not (Test-Path -LiteralPath (Join-Path $packageRoot $relativePath) -PathType Leaf)) {
                throw "Release package is missing $relativePath"
            }
        }

        New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
        $verificationRoot = Join-Path $stagingRoot 'archive-check'
        try {
            Compress-Archive -Path (Join-Path $packageRoot '*') -DestinationPath $candidateArchivePath -CompressionLevel Optimal
            Expand-Archive -LiteralPath $candidateArchivePath -DestinationPath $verificationRoot
            foreach ($relativePath in $requiredPackageFiles) {
                if (-not (Test-Path -LiteralPath (Join-Path $verificationRoot $relativePath) -PathType Leaf)) {
                    throw "Release archive is missing $relativePath"
                }
            }
            $archiveVersion = (Get-Content -LiteralPath (Join-Path $verificationRoot 'VERSION') -Raw).Trim()
            if ($archiveVersion -ne $Version) {
                throw "Release archive VERSION does not match $Version"
            }
            $archiveManifest = @(Get-Content -LiteralPath (Join-Path $verificationRoot 'BUILD.txt'))
            if ($archiveManifest.Count -ne 2 -or $archiveManifest[0] -ne "version=$Version" -or $archiveManifest[1] -ne "commit=$buildCommit") {
                throw 'Release archive BUILD.txt does not match the requested version and source commit.'
            }
            foreach ($relativePath in @('typecho-next.exe', 'web\dist\index.html')) {
                if ((Get-Item -LiteralPath (Join-Path $verificationRoot $relativePath)).Length -lt 1) {
                    throw "Release archive contains an empty $relativePath"
                }
            }
            $releaseExecutable = Join-Path $verificationRoot 'typecho-next.exe'
            $peFormat = @(Invoke-CommandChecked 'objdump' @('-f', $releaseExecutable))
            $isWindowsAMD64 = @($peFormat | Where-Object { $_ -match '^\s*.+:\s*file format pei-x86-64\s*$' }).Count -eq 1 -and
                @($peFormat | Where-Object { $_ -match '^\s*architecture:\s*i386:x86-64(?:,|\s*$)' }).Count -eq 1
            if (-not $isWindowsAMD64) {
                throw 'Release executable is not a Windows x64 PE binary.'
            }
            $peImports = @(Invoke-CommandChecked 'objdump' @('-p', $releaseExecutable))
            $mingwRuntimeDependencies = @()
            foreach ($line in $peImports) {
                if ($line -match '^\s*DLL Name:\s*(\S+)\s*$') {
                    $dllName = $Matches[1]
                    if ($dllName -match '^lib(?:gcc|winpthread|stdc\+\+|ssp|atomic|gomp|quadmath|mingwex|mingw32|mingwthrd)(?:[-_].*)?\.dll$') {
                        $mingwRuntimeDependencies += $dllName
                    }
                }
            }
            if ($mingwRuntimeDependencies.Count -gt 0) {
                throw "Release executable depends on MinGW runtime DLLs: $($mingwRuntimeDependencies -join ', ')"
            }
            Test-PackagedRuntime -PackageRoot $verificationRoot -SmokeRoot (Join-Path $stagingRoot 'runtime-smoke')
            [IO.File]::Move($candidateArchivePath, $archivePath)
        } catch {
            Remove-Item -LiteralPath $candidateArchivePath -Force -ErrorAction SilentlyContinue
            throw
        }
        Write-Output "Created release package: $archivePath"
    } finally {
        foreach ($name in $previousEnvironment.Keys) {
            $value = $previousEnvironment[$name]
            if ($null -eq $value) {
                [Environment]::SetEnvironmentVariable($name, $null, 'Process')
            } else {
                [Environment]::SetEnvironmentVariable($name, $value, 'Process')
            }
        }
        Remove-Item -LiteralPath $candidateArchivePath -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $stagingRoot) {
            $temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
            $resolvedStage = [IO.Path]::GetFullPath($stagingRoot)
            if (-not $resolvedStage.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase)) {
                throw "Refusing to remove staging directory outside the temporary directory: $resolvedStage"
            }
            Remove-Item -LiteralPath $resolvedStage -Recurse -Force
        }
    }
}

function Test-GoFormat {
    $goFiles = @(git ls-files --cached --others --exclude-standard -- '*.go')
    if ($LASTEXITCODE -ne 0) {
        throw "git ls-files exited with code $LASTEXITCODE"
    }

    $unformatted = @()
    foreach ($goFile in $goFiles) {
        $result = & gofmt -l $goFile
        if ($LASTEXITCODE -ne 0) {
            throw "gofmt failed for $goFile with code $LASTEXITCODE"
        }
        if ($result) {
            $unformatted += $result
        }
    }
    if ($unformatted.Count -gt 0) {
        throw "Run gofmt on:`n$($unformatted -join "`n")"
    }
    Write-Output 'All Go files are gofmt-formatted.'
}

Push-Location $PSScriptRoot
try {
    switch ($Task) {
        'dev' {
            Push-Location (Join-Path $PSScriptRoot 'web')
            try { Invoke-CommandChecked 'npm.cmd' @('run', 'dev') } finally { Pop-Location }
        }
        'web-build' { Build-Web }
        'api-run' { Invoke-CommandChecked 'go' @('run', './cmd/server') }
        'fmt-check' { Test-GoFormat }
        'package' { Package-Release }
        'build' {
            Build-Web
            New-Item -ItemType Directory -Force -Path (Join-Path $PSScriptRoot 'bin') | Out-Null
            Invoke-CommandChecked 'go' @('build', '-o', './bin/typecho-next.exe', './cmd/server')
        }
        'clean' {
            $cleanupTargets = @()
            foreach ($relativePath in @('web/dist', 'bin')) {
                $targetPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot $relativePath))
                $workspacePrefix = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd('\') + '\'
                if (-not $targetPath.StartsWith($workspacePrefix, [StringComparison]::OrdinalIgnoreCase)) {
                    throw "Cleanup target is outside the project: $targetPath"
                }
                if (Test-Path -LiteralPath $targetPath) {
                    $targetItem = Get-Item -LiteralPath $targetPath -Force
                    if (($targetItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                        throw "Cleanup target is a link and will not be followed: $targetPath"
                    }
                    if ($targetItem.PSIsContainer) {
                        $nestedLink = Get-ChildItem -LiteralPath $targetPath -Force -Recurse |
                            Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 } |
                            Select-Object -First 1
                        if ($null -ne $nestedLink) {
                            throw "Cleanup target contains a link and will not be followed: $($nestedLink.FullName)"
                        }
                    }
                }
                $cleanupTargets += $targetPath
            }
            foreach ($targetPath in $cleanupTargets) {
                if (Test-Path -LiteralPath $targetPath) {
                    Remove-Item -LiteralPath $targetPath -Recurse -Force
                }
            }
        }
    }
} finally {
    Pop-Location
}
