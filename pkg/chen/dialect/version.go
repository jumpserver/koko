package dialect

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MySQLCompatibilityNumber encodes VERSION() as used by MySQL executable
// comments. A vendor suffix can contain a separate product release.
func MySQLCompatibilityNumber(reported string) int {
	var major, minor, patch int
	fmt.Sscanf(reported, "%d.%d.%d", &major, &minor, &patch)
	return major*10000 + minor*100 + patch
}

// Version is the numeric product release, independent of any compatibility
// version a server advertises to its clients.
type Version struct {
	Major, Minor, Patch int
}

const releaseNumber = `(\d+)\.(\d+)\.(\d+)`

var oceanBaseRelease = regexp.MustCompile(`(?i)OceanBase[^\s]*(?:-v|\s+v?)` + releaseNumber)
var oceanBaseMySQLBanner = regexp.MustCompile(`(?i)OceanBase[^\s]*-v` + releaseNumber + `(?:[.-].*)?$`)

func HasOceanBaseBrand(banner string) bool {
	return strings.Contains(strings.ToLower(banner), "oceanbase")
}

// ParseOceanBaseVersion extracts the product release from a branded banner.
// The leading MySQL version in a combined banner describes the wire dialect.
func ParseOceanBaseVersion(banner string) (Version, bool) {
	return parseRelease(oceanBaseRelease.FindStringSubmatch(banner))
}

// ParseOceanBaseMySQLBanner reads the -v product suffix of a MySQL-mode
// VERSION() banner. A separate version comment can use the broader release format.
func ParseOceanBaseMySQLBanner(banner string) (Version, bool) {
	return parseRelease(oceanBaseMySQLBanner.FindStringSubmatch(banner))
}

func parseRelease(match []string) (Version, bool) {
	if match == nil {
		return Version{}, false
	}
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	patch, errPatch := strconv.Atoi(match[3])
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}
