package torznab_client

import (
	"testing"

	"github.com/MunifTanjim/stremthru/internal/znab"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeIMDBId(t *testing.T) {
	for raw, expected := range map[string]string{
		"tt0903747":   "tt0903747",
		"0903747":     "tt0903747",
		"903747":      "tt0903747",
		"tt14986406":  "tt14986406",
		" TT0141842 ": "tt0141842",
		"0":           "",
		"":            "",
		"tt":          "",
		"abc":         "",
		"12345678901": "",
	} {
		assert.Equal(t, expected, normalizeIMDBId(raw), raw)
	}
}

// TorrentLeech publishes "imdb" (without the tt prefix); others "imdbid".
func TestTorzFromChannelItemReadsIMDbId(t *testing.T) {
	item := &znab.ChannelItem{Title: "Breaking.Bad.S01E01.1080p"}
	torz := TorzFromChannelItem(item, znab.ChannelItemAttrs{
		{Name: znab.TorznabAttrNameIMDB, Value: "0903747"},
	})
	assert.Equal(t, "tt0903747", torz.IMDBId)

	torz = TorzFromChannelItem(item, znab.ChannelItemAttrs{
		{Name: znab.TorznabAttrNameIMDBId, Value: "tt0903747"},
		{Name: znab.TorznabAttrNameIMDB, Value: "1234567"},
	})
	assert.Equal(t, "tt0903747", torz.IMDBId, "imdbid wins over imdb")

	torz = TorzFromChannelItem(item, znab.ChannelItemAttrs{{Name: znab.TorznabAttrNameIMDB, Value: "0"}})
	assert.Equal(t, "", torz.IMDBId, "0 means unknown")
}
