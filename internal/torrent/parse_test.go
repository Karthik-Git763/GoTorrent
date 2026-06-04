package torrent

import (
	"os"
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
