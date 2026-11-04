package app

import "regexp"

var forkReleaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+-cursor\.[1-9]\d*$`)

// Fork releases are published as stable releases despite their version suffix.
// Keep their manifest on the fork repository, paired with its embedded update key.
func forkUpdateFeed(version, goos, goarch string) string {
	if !forkReleaseVersion.MatchString(version) {
		return ""
	}
	return releasesURL + "/download/update-" + goos + "-" + goarch + ".json"
}
