#!/bin/sh
# Runs inside a distro container in CI. Exit code 1 means "found a problem",
# which is fine here; a crashed check or a higher code is not.
set -u
out=$(mktemp -d)

tuxmedic check --verbose >"$out/check.txt" 2>&1
code=$?
cat "$out/check.txt"
[ "$code" -le 1 ] || { echo "smoke: tuxmedic check exited $code"; exit 1; }
if grep -q "This check crashed" "$out/check.txt"; then
	echo "smoke: a check crashed"
	exit 1
fi

tuxmedic check --json >"$out/check.json"
[ "$?" -le 1 ] || { echo "smoke: --json failed"; exit 1; }
grep -q '"checks"' "$out/check.json" || { echo "smoke: --json has no checks"; exit 1; }

tuxmedic report >"$out/report.md"
[ "$?" -le 1 ] || { echo "smoke: report failed"; exit 1; }
grep -q "tuxmedic report" "$out/report.md" || { echo "smoke: report is empty"; exit 1; }

tuxmedic watch --for 5s >"$out/watch.txt" 2>&1
[ "$?" -le 1 ] || { cat "$out/watch.txt"; echo "smoke: watch failed"; exit 1; }
grep -q "of watching\|dropped" "$out/watch.txt" || { cat "$out/watch.txt"; echo "smoke: watch printed no summary"; exit 1; }

tuxmedic ask "my wifi keeps dropping" >"$out/ask.txt" 2>&1
[ "$?" -le 1 ] || { cat "$out/ask.txt"; echo "smoke: ask failed"; exit 1; }

echo "smoke: ok"
