package version

import (
	"testing"
	"time"

	"github.com/blang/semver"
)

func TestParseVersion(t *testing.T) {
	_, err := semver.Parse(Vers)
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseTimestamp(t *testing.T) {
	_, err := time.Parse(TimestampFormat, Timestamp)
	if err != nil {
		t.Fatal(err)
	}
}
