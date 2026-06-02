package torrent

import "crypto/sha1"

func (tf *TorrentFile) computeInfoHash(rawInfoDict []byte) {
	h := sha1.Sum(rawInfoDict)
	copy(tf.InfoHash[:], h[:])
}