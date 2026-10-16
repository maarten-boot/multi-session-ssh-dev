#!/bin/bash
# Generate the package documentation that depends on the version, from git
# history since the previous tag:
#   dist/pkgdocs/changelog.gz          Debian changelog (the .deb)
#   dist/pkgdocs/changelog.yml         chglog entries for the RPM %changelog
#   dist/pkgdocs/nfpm-rpm.yaml         packaging/nfpm.yaml + that changelog
#                                      (nfpm would also put it into the .deb,
#                                      under a name wrong for this package)
#   dist/pkgdocs/session-workbench.1.gz
# usage: packaging/gen-docs.sh <version>   (run from the repository root)
set -euo pipefail
VERSION=$1
OUT=dist/pkgdocs
mkdir -p "$OUT"
# Reproducible: the date of the last commit, not the build time.
EPOCH=$(git log -1 --format=%ct)
RFC=$(LC_ALL=C date -u -d "@$EPOCH" '+%a, %d %b %Y %H:%M:%S +0000')
DAY=$(LC_ALL=C date -u -d "@$EPOCH" '+%Y-%m-%d')
CUR=$(git describe --tags --abbrev=0 2>/dev/null || true)
PREV=$(git describe --tags --abbrev=0 "${CUR}^" 2>/dev/null || true)
RANGE=${PREV:+$PREV..}HEAD
{
	echo "session-workbench ($VERSION) unstable; urgency=medium"
	echo
	# Debian changelog lines stay within 80 columns: wrap at 76, continue indented.
	git log --no-merges --format='%s' "$RANGE" | head -n 60 |
		while IFS= read -r subject; do
			printf '%s\n' "$subject" | fold -s -w 74 | sed '1s/^/  * /; 2,$s/^/    /'
		done
	echo "  * Release notes:"
	echo "    https://github.com/maarten-boot/multi-session-ssh-dev/releases"
	echo
	echo " -- Maarten Boot <11140490+maarten-boot@users.noreply.github.com>  $RFC"
} | gzip -9n >"$OUT/changelog.gz"
sed "s/@VERSION@/$VERSION/; s/@DATE@/$DAY/" packaging/session-workbench.1 | gzip -9n >"$OUT/session-workbench.1.gz"

{
	echo "- semver: $VERSION-1" # version-release, as rpmlint compares it
	echo "  date: $(date -u -d "@$EPOCH" '+%Y-%m-%dT%H:%M:%SZ')"
	echo "  packager: Maarten Boot <11140490+maarten-boot@users.noreply.github.com>"
	echo "  changes:"
	git log --no-merges --format='%s' "$RANGE" | head -n 60 | sed "s/'/''/g; s/^/    - note: '/; s/\$/'/"
} >"$OUT/changelog.yml"
sed "/^name:/a changelog: $OUT/changelog.yml" packaging/nfpm.yaml >"$OUT/nfpm-rpm.yaml"
