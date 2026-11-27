package config

import (
	"strings"
	"time"

	"github.com/MunifTanjim/stremthru/internal/util"
)

type torzConfig struct {
	TorrentFileCacheSize int64
	TorrentFileCacheTTL  time.Duration
	TorrentFileMaxSize   int64
	// ReadOnly keeps requests from queuing peer pulls or indexer searches:
	// they only read stored torrents. Workers (feed scraping) still queue.
	ReadOnly bool
}

var Torz = func() torzConfig {
	torz := torzConfig{
		TorrentFileCacheSize: util.ToBytes(getEnv("STREMTHRU_TORZ_TORRENT_FILE_CACHE_SIZE")),
		TorrentFileCacheTTL:  mustParseDuration("torz torrent file cache ttl", getEnv("STREMTHRU_TORZ_TORRENT_FILE_CACHE_TTL")),
		TorrentFileMaxSize:   util.ToBytes(getEnv("STREMTHRU_TORZ_TORRENT_FILE_MAX_SIZE")),
		ReadOnly:             strings.ToLower(getEnv("STREMTHRU_TORZ_READ_ONLY")) == "true",
	}

	return torz
}()
