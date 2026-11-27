# Torz (Torrent)

Configuration for StremThru's Torz (Torrent) functionality.

## Torz

### `STREMTHRU_TORZ_TORRENT_FILE_CACHE_SIZE`

Size of the torrent file cache.

- **Default:** `256MB`

**Example:**

```sh
STREMTHRU_TORZ_TORRENT_FILE_CACHE_SIZE=256MB
```

::: info
Disk backed cache. Make sure you have enough disk space.
:::

### `STREMTHRU_TORZ_TORRENT_FILE_CACHE_TTL`

TTL for cached torrent files.

- **Default:** `6h`

**Example:**

```sh
STREMTHRU_TORZ_TORRENT_FILE_CACHE_TTL=6h
```

### `STREMTHRU_TORZ_TORRENT_FILE_MAX_SIZE`

Maximum torrent file size allowed.

- **Default:** `1MB`

**Example:**

```sh
STREMTHRU_TORZ_TORRENT_FILE_MAX_SIZE=1MB
```

### `STREMTHRU_TORZ_READ_ONLY`

Read-only mode for requests. Stremio Torz, Wrap and Torznab API requests
only read the torrents already stored: they do not pull torrents from peers
and do not queue indexer searches. Workers still fill the database, such as
the Torznab feed scraper, which queues an indexer search for every IMDb id
an opted-in indexer's feed publishes (TorrentLeech does).

- **Default:** `false`

**Example:**

```sh
STREMTHRU_TORZ_READ_ONLY=true
```
