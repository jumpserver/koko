package dialect

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MySQLCompatibilityNumber encodes VERSION() as used by MySQL executable
// comments. Missing minor or patch components retain their zero values.
// A vendor suffix can contain a separate product release.
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

var numericRelease = regexp.MustCompile(`^` + releaseNumber)
var oceanBaseRelease = regexp.MustCompile(`(?i)OceanBase[^\s]*(?:-v|\s+v?)` + releaseNumber)
var oceanBaseMySQLBanner = regexp.MustCompile(`(?i)OceanBase[^\s]*-v` + releaseNumber + `(?:[.-].*)?$`)

// ParseNumericVersion reads the three leading decimal components and returns
// the remaining suffix for callers that require a particular delimiter.
// Oversized components saturate at MaxInt, preserving existing range checks
// for MariaDB and ClickHouse (including checks that only use the major).
func ParseNumericVersion(banner string) (Version, string, bool) {
	match := numericRelease.FindStringSubmatch(banner)
	if match == nil {
		return Version{}, "", false
	}
	v, _ := releaseNumbers(match)
	return v, banner[len(match[0]):], true
}

func HasMariaDBBrand(banner string) bool {
	return strings.Contains(strings.ToLower(banner), "mariadb")
}

// ParseMariaDBVersion strips the optional MySQL-compatible prefix from a branded release.
func ParseMariaDBVersion(banner string) (Version, string, bool) {
	if !HasMariaDBBrand(banner) {
		return Version{}, "", false
	}
	return ParseNumericVersion(strings.TrimPrefix(banner, "5.5.5-"))
}

// ParseSQLServerMajor reads the major component followed by a dot in ProductVersion.
func ParseSQLServerMajor(version string) (int, bool) {
	var major int
	_, err := fmt.Sscanf(version, "%d.", &major)
	return major, err == nil
}

// ParseDB2ServiceLevel reads major.minor with an optional DB2 V prefix.
func ParseDB2ServiceLevel(level string) (Version, bool) {
	version := strings.TrimSpace(level)
	if strings.HasPrefix(strings.ToUpper(version), "DB2 V") {
		version = version[5:]
	}
	var v Version
	_, err := fmt.Sscanf(version, "%d.%d", &v.Major, &v.Minor)
	return v, err == nil
}

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
	if match != nil {
		if v, exact := releaseNumbers(match); exact {
			return v, true
		}
	}
	return Version{}, false
}

func releaseNumbers(match []string) (Version, bool) {
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	patch, errPatch := strconv.Atoi(match[3])
	return Version{Major: major, Minor: minor, Patch: patch}, errMajor == nil && errMinor == nil && errPatch == nil
}
