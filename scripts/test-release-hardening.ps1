#requires -Version 5.1

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "TwinTidy.Release.ps1")

function Assert-Throws {
    param(
        [Parameter(Mandatory = $true)][scriptblock]$Action,
        [Parameter(Mandatory = $true)][string]$Description
    )

    try {
        & $Action
    } catch {
        return $_.Exception.Message
    }
    throw "Expected failure was not raised: $Description"
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("TwinTidyReleaseTests-" + [System.Guid]::NewGuid().ToString("N"))
[System.IO.Directory]::CreateDirectory($tempRoot) | Out-Null
try {
    $unsafeManifest = Join-Path $tempRoot "comment-bypass.manifest"
    $unsafeXML = @'
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <!-- requestedExecutionLevel level="asInvoker" uiAccess="false" -->
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges>
      <requestedExecutionLevel level="requireAdministrator" uiAccess="true"/>
    </requestedPrivileges></security>
  </trustInfo>
</assembly>
'@
    [System.IO.File]::WriteAllText($unsafeManifest, $unsafeXML, [System.Text.UTF8Encoding]::new($false))
    $manifestFailure = Assert-Throws `
        -Action { $null = Assert-TwinTidyManifestPolicy -Path $unsafeManifest } `
        -Description "comment text must not satisfy the active manifest policy"

    $duplicateManifest = Join-Path $tempRoot "duplicate-policy.manifest"
    $duplicateXML = @'
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges>
      <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
    </requestedPrivileges></security>
  </trustInfo>
</assembly>
'@
    [System.IO.File]::WriteAllText($duplicateManifest, $duplicateXML, [System.Text.UTF8Encoding]::new($false))
    $duplicateManifestFailure = Assert-Throws `
        -Action { $null = Assert-TwinTidyManifestPolicy -Path $duplicateManifest } `
        -Description "duplicate requestedExecutionLevel elements must fail closed"

    $testRepository = Join-Path $tempRoot "source-repository"
    [System.IO.Directory]::CreateDirectory($testRepository) | Out-Null
    Push-Location $testRepository
    try {
        & git init --quiet
        if ($LASTEXITCODE -ne 0) { throw "Unable to initialize release-gate fixture repository." }
        & git config user.name "TwinTidy Release Test"
        & git config user.email "release-test@invalid.example"
        & git config core.autocrlf false
        [System.IO.File]::WriteAllText((Join-Path $testRepository "tracked.txt"), "reviewed`n", [System.Text.UTF8Encoding]::new($false))
        & git add -- tracked.txt
        & git commit --quiet -m "fixture"
        if ($LASTEXITCODE -ne 0) { throw "Unable to commit release-gate fixture." }
    } finally {
        Pop-Location
    }
    $null = Assert-TwinTidyReleaseSourceClean -RepositoryRoot $testRepository

    [System.IO.File]::WriteAllText((Join-Path $testRepository "tracked.txt"), "modified`n", [System.Text.UTF8Encoding]::new($false))
    $modifiedFailure = Assert-Throws `
        -Action { $null = Assert-TwinTidyReleaseSourceClean -RepositoryRoot $testRepository } `
        -Description "modified tracked release source must fail closed"
    Push-Location $testRepository
    try {
        & git restore --source=HEAD --worktree -- tracked.txt
        if ($LASTEXITCODE -ne 0) { throw "Unable to restore release-gate fixture." }
    } finally {
        Pop-Location
    }
    [System.IO.File]::WriteAllText((Join-Path $testRepository "untracked.go"), "package injected`n", [System.Text.UTF8Encoding]::new($false))
    $untrackedFailure = Assert-Throws `
        -Action { $null = Assert-TwinTidyReleaseSourceClean -RepositoryRoot $testRepository } `
        -Description "untracked release source must fail closed"

    $version = "dev"
    $architecture = "amd64"
    $sourceDate = "2026-07-10T00:00:00Z"
    $inputRoot = Join-Path $tempRoot "package-input"
    $buildDirectory = Join-Path $inputRoot "TwinTidy-$version-windows-$architecture"
    [System.IO.Directory]::CreateDirectory($buildDirectory) | Out-Null
    $executablePath = Join-Path $buildDirectory "TwinTidy.exe"
    $receiptPath = Join-Path $buildDirectory "TwinTidy.build-receipt.json"
    [System.IO.File]::WriteAllBytes($executablePath, [System.Text.Encoding]::ASCII.GetBytes("verified executable bytes"))
    $executableHash = Get-TwinTidyFileSHA256 -Path $executablePath
    $receipt = [ordered]@{
        schema = "twintidy.build-receipt/v1"
        product = "TwinTidy"
        version = $version
        architecture = $architecture
        sourceDate = $sourceDate
        source = [ordered]@{
            kind = "git-commit"
            commit = ("a" * 40)
            gitTree = ("b" * 40)
            clean = $true
            treeDigestAlgorithm = "sha256-path-length-v1"
            treeSHA256 = ("c" * 64)
            fileCount = 1
        }
        build = [ordered]@{
            goVersion = "go fixture"
            goos = "windows"
            goarch = $architecture
            cgoEnabled = $false
            trimpath = $true
            buildVCS = $false
            resourceSHA256 = ("d" * 64)
        }
        executable = [ordered]@{
            path = "TwinTidy.exe"
            size = ([System.IO.FileInfo]::new($executablePath)).Length
            sha256 = $executableHash
        }
    }
    [System.IO.File]::WriteAllText(
        $receiptPath,
        (($receipt | ConvertTo-Json -Depth 10) + "`n"),
        [System.Text.UTF8Encoding]::new($false)
    )
    $receiptHash = Get-TwinTidyFileSHA256 -Path $receiptPath
    $expectedExecutable = @{ amd64 = $executableHash }
    $expectedReceipt = @{ amd64 = $receiptHash }
    $packageOutput = Join-Path $tempRoot "package-output"
    $packageResults = @(& (Join-Path $PSScriptRoot "package.ps1") `
        -Version $version `
        -Architecture $architecture `
        -InputDirectory $inputRoot `
        -OutputDirectory $packageOutput `
        -SourceDate $sourceDate `
        -ExpectedExecutableSHA256 $expectedExecutable `
        -ExpectedBuildReceiptSHA256 $expectedReceipt)
    $archiveResult = @($packageResults | Where-Object { $_.Architecture -ceq $architecture })
    if ($archiveResult.Count -ne 1 -or -not [System.IO.File]::Exists($archiveResult[0].Path)) {
        throw "Digest-bound package fixture did not produce exactly one archive."
    }

    [System.IO.File]::WriteAllBytes($executablePath, [System.Text.Encoding]::ASCII.GetBytes("replacement executable bytes"))
    $artifactFailure = Assert-Throws `
        -Action {
            $null = & (Join-Path $PSScriptRoot "package.ps1") `
                -Version $version `
                -Architecture $architecture `
                -InputDirectory $inputRoot `
                -OutputDirectory (Join-Path $tempRoot "rejected-package") `
                -SourceDate $sourceDate `
                -ExpectedExecutableSHA256 $expectedExecutable `
                -ExpectedBuildReceiptSHA256 $expectedReceipt
        } `
        -Description "replacement executable must not be coherently packaged and checksummed"
    if ($artifactFailure -notmatch 'Executable SHA-256.+does not match expected') {
        throw "Replacement fixture failed for an unexpected reason: $artifactFailure"
    }

    [pscustomobject]@{
        ManifestCommentBypassRejected = -not [string]::IsNullOrWhiteSpace($manifestFailure)
        DuplicateManifestPolicyRejected = -not [string]::IsNullOrWhiteSpace($duplicateManifestFailure)
        ModifiedSourceRejected = -not [string]::IsNullOrWhiteSpace($modifiedFailure)
        UntrackedSourceRejected = -not [string]::IsNullOrWhiteSpace($untrackedFailure)
        VerifiedPackageCreated = $true
        MismatchedArtifactRejected = -not [string]::IsNullOrWhiteSpace($artifactFailure)
    }
} finally {
    $fullTempRoot = [System.IO.Path]::GetFullPath($tempRoot)
    $systemTempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    if (-not $systemTempRoot.EndsWith([System.IO.Path]::DirectorySeparatorChar.ToString())) {
        $systemTempRoot += [System.IO.Path]::DirectorySeparatorChar
    }
    if (-not $fullTempRoot.StartsWith($systemTempRoot, [System.StringComparison]::OrdinalIgnoreCase) -or
        -not ([System.IO.Path]::GetFileName($fullTempRoot)).StartsWith("TwinTidyReleaseTests-", [System.StringComparison]::Ordinal)) {
        throw "Refusing to remove unexpected test directory: $fullTempRoot"
    }
    if ([System.IO.Directory]::Exists($fullTempRoot)) {
        Get-ChildItem -LiteralPath $fullTempRoot -Recurse -Force | ForEach-Object {
            $_.Attributes = [System.IO.FileAttributes]::Normal
        }
        (Get-Item -LiteralPath $fullTempRoot -Force).Attributes = [System.IO.FileAttributes]::Directory
        [System.IO.Directory]::Delete($fullTempRoot, $true)
    }
}
