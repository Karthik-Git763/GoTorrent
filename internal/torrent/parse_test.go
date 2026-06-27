package torrent

import (
	"crypto/sha1"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestParseRealTorrent(t *testing.T) {
	data, err := os.ReadFile("testdata/big-buck-bunny.torrent")
	if err != nil {
		t.Fatal(err)
	}

	var tf TorrentFile
	if err := tf.Parse(data); err != nil {
		t.Fatal(err)
	}

	if tf.Name == "" || tf.PieceLength == 0 || len(tf.PieceHashes) == 0 {
		t.Fatalf("bad parse: %+v", tf)
	}
}

func TestParseWebSeedFields(t *testing.T) {
	hash := sha1.Sum([]byte("test"))
	raw := fmt.Sprintf("d8:announce14:http://tracker8:url-listl14:http://seed/a/14:http://seed/b/e9:httpseedsl19:http://seed/script/e4:infod6:lengthi4e4:name4:test12:piece lengthi4e6:pieces20:%see", string(hash[:]))

	var tf TorrentFile
	if err := tf.Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}

	if got, want := strings.Join(tf.URLList, ","), "http://seed/a/,http://seed/b/"; got != want {
		t.Fatalf("URLList = %q, want %q", got, want)
	}
	if got, want := strings.Join(tf.HTTPSeeds, ","), "http://seed/script/"; got != want {
		t.Fatalf("HTTPSeeds = %q, want %q", got, want)
	}
}

func TestParseURLListString(t *testing.T) {
	hash := sha1.Sum([]byte("test"))
	raw := fmt.Sprintf("d8:url-list16:http://seed/file4:infod6:lengthi4e4:name4:test12:piece lengthi4e6:pieces20:%see", string(hash[:]))

	var tf TorrentFile
	if err := tf.Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if len(tf.URLList) != 1 || tf.URLList[0] != "http://seed/file" {
		t.Fatalf("URLList = %#v", tf.URLList)
	}
}

func TestParseRejectsInvalidFileModeMix(t *testing.T) {
	hash := sha1.Sum([]byte("test"))
	raw := fmt.Sprintf("d4:infod5:filesld6:lengthi4e4:pathl8:file.txteee6:lengthi4e4:name4:test12:piece lengthi4e6:pieces20:%see", string(hash[:]))

	var tf TorrentFile
	if err := tf.Parse([]byte(raw)); err == nil {
		t.Fatal("expected error for torrent with both length and files")
	}
}
