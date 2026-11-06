package worker

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MunifTanjim/stremthru/internal/anidb"
	"github.com/MunifTanjim/stremthru/internal/matchlab"
	"github.com/MunifTanjim/stremthru/internal/torrent_info"
	"github.com/MunifTanjim/stremthru/internal/torrent_stream"
)

func matchLabPointer(s string) *string { return &s }
func mkSID(first, last string) matchlab.Response {
	return matchlab.Response{SIDStart:matchLabPointer(first),SIDEnd:matchLabPointer(last)}
}

func TestMatchLabSingleSeasonEnumeratesEpisodeInterval(t *testing.T) {
	pack := torrent_info.TorrentInfo{
		Hash:"abc", TorrentTitle:"Supernatural.S02",
		Seasons:torrent_info.CommaSeperatedInt{2},
	}
	whole := mkSID("tt0460681:2:1", "tt0460681:2:22")
	files := []matchLabFileResult{
		{Path:"/Supernatural/S02E01.mkv", Match:mkSID("tt0460681:2:1","tt0460681:2:1")},
		{Path:"/Supernatural/S02E03.mkv", Match:mkSID("tt0460681:2:3","tt0460681:2:3")},
	}
	out := buildMatchLabMapping(pack,whole,files,nil)
	if !reflect.DeepEqual(out.IMDbIDs,[]string{"tt0460681"}) { t.Fatalf("IMDb IDs: %v",out.IMDbIDs) }
	if !reflect.DeepEqual(out.Seasons,[]int{2}) { t.Fatalf("seasons: %v",out.Seasons) }
	if !reflect.DeepEqual(out.Episodes,[]int{1,2,3}) { t.Fatalf("episodes: %v",out.Episodes) }
	if out.Category != torrent_info.TorrentInfoCategorySeries { t.Fatal("expected series") }
}

func TestMatchLabSingleSeasonFromVerifiedPackEndpoints(t *testing.T) {
	pack := torrent_info.TorrentInfo{Hash:"abc",TorrentTitle:"Anime.Title.1-7"}
	whole := mkSID("tt1234567:1:1","tt1234567:1:7")
	out := buildMatchLabMapping(pack,whole,nil,nil)
	if !reflect.DeepEqual(out.Seasons,[]int{1}) { t.Fatalf("seasons: %v",out.Seasons) }
	if !reflect.DeepEqual(out.Episodes,[]int{1,2,3,4,5,6,7}) {
		t.Fatalf("episodes: %v",out.Episodes)
	}
}

func TestMatchLabMultiSeasonDropsUnscopedEpisodes(t *testing.T) {
	pack := torrent_info.TorrentInfo{
		Hash:"abc",TorrentTitle:"Supernatural S01-S03",
		Seasons:torrent_info.CommaSeperatedInt{1,2,3},
		Episodes:torrent_info.CommaSeperatedInt{1,2,3},
	}
	whole := mkSID("tt0460681:1:1","tt0460681:3:22")
	files := []matchLabFileResult{
		{Path:"/Supernatural/S01E01.mkv",Match:mkSID("tt0460681:1:1","tt0460681:1:1")},
		{Path:"/Supernatural/S03E22.mkv",Match:mkSID("tt0460681:3:22","tt0460681:3:22")},
	}
	out := buildMatchLabMapping(pack,whole,files,nil)
	if !reflect.DeepEqual(out.Seasons,[]int{1,2,3}) { t.Fatalf("seasons: %v",out.Seasons) }
	if len(out.Episodes) != 0 { t.Fatalf("multi-season must leave episodes blank: %v",out.Episodes) }
}

func TestMatchLabMixedAnimeIdentitiesAreIndependent(t *testing.T) {
	pack := torrent_info.TorrentInfo{Hash:"abc",TorrentTitle:"Anime.Collection"}
	files := []matchLabFileResult{
		{
			Path:"/A/01.mkv",
			Match:matchlab.Response{
				SIDStart:matchLabPointer("tt1234567:1:1"),SIDEnd:matchLabPointer("tt1234567:1:1"),
				ASIDStart:matchLabPointer("100:1"),ASIDEnd:matchLabPointer("100:1"),
			},
		},
		{
			Path:"/B/01.mkv",
			Match:matchlab.Response{
				SIDStart:matchLabPointer("tt2345678:1:1"),SIDEnd:matchLabPointer("tt2345678:1:1"),
				ASIDStart:matchLabPointer("200:1"),ASIDEnd:matchLabPointer("200:1"),
			},
		},
	}
	out := buildMatchLabMapping(pack,matchlab.Response{},files,map[string]int{"100":1,"200":2})
	if !reflect.DeepEqual(out.IMDbIDs,[]string{"tt1234567","tt2345678"}) { t.Fatalf("IMDb IDs: %v",out.IMDbIDs) }
	if len(out.Seasons) != 0 || len(out.Episodes) != 0 { t.Fatalf("mixed collection cannot have one global episode range: %+v",out) }
	if len(out.AniDB) < 2 { t.Fatalf("missing anime mappings: %+v",out.AniDB) }
	seen := map[string]bool{}
	for _, a := range out.AniDB {
		if a.SeasonType != anidb.TorrentSeasonTypeAnime { continue }
		seen[a.TId] = true
		if a.EpisodeStart != 1 || a.EpisodeEnd != 1 { t.Fatalf("unexpected AniDB range: %+v",a) }
	}
	if !seen["100"] || !seen["200"] { t.Fatalf("missing independent anime IDs: %v",seen) }
}

func TestMatchLabAnonymousAnimeSeasonSentinel(t *testing.T) {
	pack := torrent_info.TorrentInfo{Hash:"abc",TorrentTitle:"Anime"}
	match := matchlab.Response{ASIDStart:matchLabPointer("42:7"),ASIDEnd:matchLabPointer("42:7")}
	out := buildMatchLabMapping(pack,match,nil,nil)
	found := false
	for _, a := range out.AniDB {
		if a.SeasonType == anidb.TorrentSeasonTypeAnime && a.TId == "42" {
			found = true
			if a.Season != -1 { t.Fatalf("expected -1 unknown AniDB season, got %d",a.Season) }
		}
	}
	if !found { t.Fatalf("no anime mapping: %+v",out.AniDB) }
}

func TestMatchLabCombinedPath(t *testing.T) {
	file := torrent_stream.File{Path:"/Show/S02E04.mkv"}
	got := matchlabFilePath("Supernatural S01-S12",file)
	if got != "Supernatural S01-S12/Show/S02E04.mkv" {
		t.Fatalf("wrong combined title: %s",got)
	}
	if !strings.Contains(got,"S02E04") { t.Fatal("child episode was removed") }
}

func TestMatchLabNoInventedFileSID(t *testing.T) {
	r := mkSID("tt0460681:1:1","tt0460681:1:7")
	if r.SingleSID() != "" { t.Fatal("cannot assign a range to a single file") }
}
