#!/bin/sh
# Runs the library's tests and then each scenario's.
#
# Scenarios are separate modules, so `go test ./...` at the root does not reach
# them. Every module runs even after one fails: a run that stops at the first
# failure tells you less than one that tells you which of them failed.
set -u

status=0

echo "===== library"
go test ./... || status=1

for manifest in testing_environment/scenarios/*/go.mod; do
	[ -f "$manifest" ] || continue
	scenario=$(dirname "$manifest")
	echo "===== $scenario"
	(cd "$scenario" && go test ./...) || status=1
done

echo "exit: $status"
exit "$status"