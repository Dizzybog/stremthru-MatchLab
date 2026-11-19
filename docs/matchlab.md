# MatchLab: torrent parser and identity matcher

This fork can use [MatchLab](https://github.com/Dizzybog/MatchLab) as the authoritative external torrent parser and IMDb/AniDB matcher. With no URL configured, StremThru retains its original Go-PTT, IMDb and AniDB workers.

## Enable

MatchLab works with StremThru's default disabled `anime` feature. It supplies its own verified IMDb/AniDB identities. Enabling `+anime` activates local anime dataset sync and related workers, **not** MatchLab itself. The optional `+performance` flag controls the asynchronous Torz request path independently.

Set the following in the StremThru container environment:

```env
STREMTHRU_INTEGRATION_MATCHLAB_URL=http://matchlab:8765
STREMTHRU_INTEGRATION_MATCHLAB_TIMEOUT=10s
STREMTHRU_INTEGRATION_MATCHLAB_CONCURRENCY=8
```

`MATCHLAB_URL` is supported as an alias for the URL. Both containers must be on a Docker network that resolves `matchlab`. The endpoint must be the **base URL**; do not include `/match`, `/parse` or credentials. MatchLab must return HTTP 200 from `/health/ready` before processing. Run migrations before starting workers.

For StremThru's standard Compose configuration, insert the three environment variables under the existing StremThru service; leave Postgres, Redis and Traefik unchanged.

## Data flow and source of truth

1. Torrent discovery/import upserts `torrent_info` with raw `t_title` and optionally `torrent_stream` file paths `p`. When MatchLab is enabled, source-provided `sid/asid` identities are stripped at import; only MatchLab may assign them. Existing verified IDs survive ordinary metadata refreshes.
2. The parse worker calls MatchLab `POST /parse` with the **full raw title**. It stores the ordinary parser fields in `torrent_info`, including normalized title, year, seasons and episodes. The parser version marks externally parsed data. This parsing is incremental; enabling MatchLab does **not** bulk-clear the parser input of every historical torrent.
3. The mapping worker calls MatchLab `POST /match` with raw `t_title`, then separately with `t_title + "/" + torrent_stream.p` **for each supported video file (also `.vob`/`.iso`)**. For anime, a separate path-only match can disambiguate mixed anime from the package name. A successful MatchLab result is never replaced by a local IMDb fuzzy search or AniDB fuzzy title matcher.
4. File-specific `sid` and `asid` are written only when the respective start and end identities are the **same single coordinate**. Multi-episode files are never incorrectly labeled as a single episode. A missing MatchLab file identity removes stale imported identities: `sid` becomes `*` and `asid` becomes empty.
5. `imdb_torrent.tid` contains the verified bare IMDb title IDs (e.g. `tt0460681`), one per independently identified work. Mixed anime packages can therefore have multiple IMDb IDs for one hash.
6. `torrent_info.seasons` is a comma-separated ascending list (`1,2,3`). If a torrent spans multiple seasons, its `episodes` field is **always empty** because that field does not encode which season owns each number. If it is a single season, `episodes` is populated across the verified range (e.g. `1,2,3,4,5,6,7`), capped to StremThru's 2,048-value field.
7. `anidb_torrent` receives one mapping per AniDB ID, mapping convention (`ani`, verified `tv`, or verified absolute `abs`) and season. The mapped `ep_start`, `ep_end`, and `eps` are derived from verified coordinates. When endpoints represent a crosswalk-spanning anime range, StremThru reads `POST /debug/match` to retrieve **verified contiguous crosswalk segments** rather than guessing where a season or anime ID changes. `eps` is used for exact episode lookups; missing middle episodes are not inferred. Unknown AniDB season is represented by `-1`, following StremThru's lookup convention.
8. A single SQL transaction updates `torrent_info`, replaces authoritative rows for the hash in `imdb_torrent` and `anidb_torrent`, writes verified file IDs, and marks `matchlab_mapped_at`. The torrent-stream cache is invalidated after commit. Transport/metadata errors roll back all changes and leave the torrent retryable.

**Episode numbering:** MatchLab's verified anime season coordinates can follow the TVDB-style crosswalk, which is not guaranteed to equal canonical IMDb numbering. The application preserves the returned identity convention and never manufactures an unverified conversion.

### Picking the series in a pack

Matching every video path is stricter than sampling only one: the highest-episode file in a single season and highest-season file in a multi-season pack are included automatically, even when filenames are not ordered. Torrent-level identification is accepted only when it agrees with the independently identified file title. With multiple different IMDb IDs, the torrent has no single global seasons/episodes list; each video retains its own SID/ASID. This prevents a mixed anime bundle from inheriting the wrong identity from its folder.

### Compatibility

- **URL absent:** the original Go-PTT parser and local IMDb mapper run, and the native AniDB mapper runs when `+anime` is enabled. Rows previously parsed by MatchLab are reparsed incrementally. Before their native parser version is stored, the parser worker removes only those hashes' old MatchLab IMDb/AniDB mappings and file SID/ASID identities, making them eligible for native remapping. All other torrents and existing native mappings remain intact. This handover can temporarily reduce results while the local workers rebuild mappings.
- **URL present:** local IMDb and AniDB matching workers are not run. External requests are bounded (default eight simultaneous HTTP calls; worker processes at most four torrent hashes in parallel). A MatchLab outage does **not** silently fall back to the local matcher. Non-exact Torznab result filtering and immediate display parsing use MatchLab `/parse` (with a per-request title cache); the legacy stream path no longer guesses an episode from filenames.
- **Existing data:** all existing hashes initially have `matchlab_mapped_at IS NULL`, so they are progressively reprocessed. Fresh file records clear this marker for their hash, so new `torrent_stream` data is re-matched. You can monitor the remaining backlog with:

```sql
SELECT count(*) FROM torrent_info
WHERE matchlab_mapped_at IS NULL
  AND t_title = parser_input;
```

- **Custom PostgreSQL triggers:** some installations have custom IMDb/anime triggers created outside StremThru's migrations. Audit and disable those separately *before* turning on the external mapper to avoid a second identity engine modifying the same records. This fork does not automatically drop user-defined triggers.
- **Existing metadata sync:** the IMDb/AniDB source-data sync jobs are unchanged. MatchLab can map without waiting for their daily synchronization. AniDB title metadata is used only to obtain AniDB season labels when available.
- **Anime flag off:** this is StremThru's default. MatchLab still writes verified AniDB mappings and ASID file identities. If AniDB title/season datasets are unavailable, the mapper stores an unknown season (`-1`), and AniDB lookups accept unknown-season mappings. IMDb anime-only Torznab indexers first use existing `anime_id_map` or verified per-file SID/ASID identities, then ask MatchLab to classify the requested IMDb title if needed. A MatchLab classification failure does not block regular indexers.
- **Anime flag on:** local AnimeAPI, AniDB titles and crosswalk synchronization remain active as before. MatchLab owns external identity matching in either mode; native anime matching is not started when MatchLab is enabled.
- **Scaling:** use a network-local MatchLab URL, sensible client concurrency for your server capacity, and monitoring of parser/mapping backlog and error logs. The initial historical mapping can produce substantial API traffic. Immediate Torznab search filtering also performs MatchLab `/parse` requests for non-exact hits; keep indexer result caps and MatchLab concurrency sensible.
- **Incremental safety:** `matchlab_file_revision` increments when torrent files change. An in-flight mapping is committed only if its file revision is still current, avoiding lost file updates. Failed parsing and matching are deferred through `matchlab_parse_retry_at` and `matchlab_retry_at`; newly updated files are immediately eligible for reprocessing.
- **File identities:** source-derived and request-derived Stremio `sid/asid` tagging is disabled in MatchLab mode. A completed MatchLab pass removes obsolete file IDs where no verified identity exists, instead of leaking stale mappings from the old engine. Imported DVD `.vob` and disc `.iso` files are eligible for MatchLab matching, without treating them as ordinary video files throughout StremThru.
- **Unmatched titles:** a successfully completed pass with no verified IMDb or AniDB ID leaves `matchlab_mapped_at` empty and schedules a retry after seven days. This allows newly learned MatchLab rules to improve previously unmatched titles without continuously retrying during normal backlog processing. Previously completed no-match rows created by older versions may require manual reset to join this schedule.
- **Bitmagnet:** when MatchLab is enabled, Bitmagnet ingestion does not wait for local IMDb metadata synchronization. The local sync prerequisite remains in native matching mode.

### Reprocessing selected torrents

The dashboard's regular **Reprocess Torrents** endpoint is also MatchLab-aware. With MatchLab enabled, it does **not** call the native IMDb or AniDB search helpers or insert blank IMDb rows. Instead it atomically clears the selected hashes' IMDb/AniDB mappings and `torrent_stream.sid/asid`, resets their parser and mapper progress, and schedules the existing MatchLab parse and mapping workers. The response is always `mode: "async"` in this mode, even for one hash; file data and other hashes are preserved. Since a MatchLab mapping is one authoritative result, the `targets` filter is not applied in MatchLab mode. Native reprocessing remains unchanged when MatchLab is disabled.

## Reset mappings from the dashboard

When MatchLab is configured, open **Dashboard → Workers → Map IMDb/AniDB via MatchLab** and select **Reset MatchLab Mapping**. Confirming clears all derived `imdb_torrent` and `anidb_torrent` mappings, clears stored `torrent_stream.sid/asid` values, and sets `torrent_info.matchlab_mapped_at` and retry markers back to pending. File paths, video metadata, hashes and the torrent catalogue are preserved. In-flight MatchLab mapping commits are fenced by incrementing `matchlab_file_revision` for every torrent. The file-list cache is invalidated on the instance performing the reset.

The optional **Also reset parsing** checkbox additionally clears `parser_input`, `parser_version` and `parsed_at`, so the parse worker will run `/parse` again before the mapping worker executes `/match`. If unchecked, only identity matching is rebuilt. The **database reset itself runs asynchronously**, because updating millions of files can exceed the reverse proxy's request timeout. The reset job and its heartbeat/status are stored in `job_log` under `reset-matchlab`. Concurrent resets are rejected. The normal parsing/mapping workers rebuild the backlog on their scheduled runs after the reset completes.

### Temporary write pause during reset

The reset first waits for active **StremThru writes and database transactions in the current process** to finish. It then holds a process-local maintenance write gate for the entire atomic reset; new torrent imports, parsing writes, matching writes, and other application writes wait until the transaction commits or rolls back. Scheduled workers pause at startup, and the active MatchLab parser/mapper pause between batches to avoid unnecessary API requests during reset. This is a cooperative pause: workers/containers are **not killed**, and the REST dashboard remains available for status polling. The reset's own `job_log` heartbeat is exempt, so the dashboard stays informed during a long reset. Writers are automatically resumed on both success and failure. Read queries can still run but PostgreSQL may block some of them while `TRUNCATE` holds table locks.

**Multiple StremThru replicas or other independent PostgreSQL writers:** The in-process write gate does not pause writes in separate containers or applications. The existing global reset advisory lock only serializes concurrent resets, not arbitrary external writers. If multiple StremThru replicas share the same database, stop or put all other writer replicas into maintenance mode during reset to avoid cross-process lock contention. This change intentionally does not terminate PostgreSQL backends or Docker containers.

The authenticated `POST /dash/api/workers/map-imdb-torrent/matchlab/reset` takes JSON `{"include_parsing":true}` (or `false`) and immediately returns **HTTP 202 Accepted** with a job ID and `status: "started"`. `GET` on the same path reports `idle`, `started`, `done`, `failed` or `interrupted` plus `torrents` and `stream_files` counts when done. The dashboard polls the status and disables the button during a running job. A stale heartbeat after a process restart is reported as interrupted, so you can restart the reset safely. A completed reset may leave torrent results temporarily unavailable while workers rebuild mappings.

When the reverse proxy returns an HTML 404/502/504 error page instead of the expected JSON, the dashboard now reports the HTTP status and path, rather than just `Unsupported Content-Type: text/html`. If the request was accepted but the job later failed, the dashboard displays the error from the persisted job status. The endpoint is unavailable when MatchLab is disabled. Reset is global, including all existing mappings, so perform a database backup first if you might need to restore older results.

## Validation

The unit tests cover HTTP response validation, failures and malformed configurations, single-/multi-season metadata, mixed anime IDs, exact anime crosswalk segments, and combined file paths. Run from the project root:

```sh
STREMTHRU_ENV=test go test --tags fts5 ./internal/matchlab ./internal/worker ./internal/torrent_info ./internal/torrent_stream
go vet ./internal/matchlab ./internal/worker ./internal/torrent_info ./internal/torrent_stream
test -z "$(gofmt -l internal/matchlab internal/worker/map_matchlab_torrent.go internal/worker/map_matchlab_torrent_test.go internal/worker/torrent_parser.go internal/torrent_info/db.go internal/torrent_stream/db.go)"
```

Then verify against a **test** PostgreSQL database with synthetic hash rows representing a movie, normal episode, single-season pack, multi-season pack, anime episode ranges (including 1-7), anime split across TV seasons and mixed anime. Verify the query paths for IMDb and AniDB episodes, not just that writes succeeded.

## Known boundary

The public MatchLab API returns only verified start/end coordinates, not complete release file membership. For actual file lists, StremThru enumerates only the video files that are present in `torrent_stream`. If only a torrent name is known (no file list), MatchLab season-level metadata is used, with the documented limitations of that source. No unmatched or unavailable external identity is invented.
