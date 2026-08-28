# Runs the library's tests and then each scenario's. See test-all.sh; this is
# the same walk for the Windows image, where there is no shell to run it with.
$status = 0

Write-Host "===== library"
go test ./...
if ($LASTEXITCODE -ne 0) { $status = 1 }

Get-ChildItem "testing_environment/scenarios" -Directory | ForEach-Object {
	if (-not (Test-Path (Join-Path $_.FullName "go.mod"))) { return }
	Write-Host "===== $($_.Name)"
	Push-Location $_.FullName
	go test ./...
	if ($LASTEXITCODE -ne 0) { $script:status = 1 }
	Pop-Location
}

Write-Host "exit: $status"
exit $status