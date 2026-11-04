package app

import "testing"

func TestForkUpdateFeedUsesOwnStableRelease(t *testing.T) {
	for _, version := range []string{"1.11.2-cursor.1", "1.11.2-cursor.12"} {
		for _, target := range [][2]string{{"darwin", "arm64"}, {"windows", "amd64"}, {"linux", "arm64"}} {
			want := "https://github.com/fan776783/Quosparr/releases/latest/download/update-" + target[0] + "-" + target[1] + ".json"
			if got := updateFeed(version, target[0], target[1]); got != want {
				t.Errorf("updateFeed(%q, %q, %q) = %q, want %q", version, target[0], target[1], got, want)
			}
		}
	}
}

func TestForkUpdateFeedDoesNotCaptureOtherVersions(t *testing.T) {
	for _, version := range []string{"1.11.2", "1.12.0-beta.1", "1.11.2-cursor.0", "1.11.2-cursor.1-beta.1"} {
		if got := forkUpdateFeed(version, "darwin", "arm64"); got != "" {
			t.Errorf("forkUpdateFeed(%q) = %q", version, got)
		}
	}
}
