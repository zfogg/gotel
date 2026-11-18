$ErrorActionPreference = 'Stop'

foreach ($tool in @('cmake', 'ninja', 'python')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "Required Windows build tool not found: $tool"
    }
}

Write-Host 'CMake, Ninja, and Python are available.'
