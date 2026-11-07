package stremio_torz

import (
	"sync"
	"time"

	"github.com/MunifTanjim/stremthru/internal/cache"
	"github.com/MunifTanjim/stremthru/internal/store"
	"github.com/MunifTanjim/stremthru/internal/util"
)

// A shared cache of completed availability checks, scoped by the user's
// configuration and stream ID. No provider credentials are stored as keys.
type magnetAvailabilitySnapshot struct {
	ByHash       map[string]string
	FailedStores map[string]struct{}
	Count        int
}

type magnetAvailabilityJob struct {
	key      string
	sid      string
	ip       string
	hashes   []string
	userData *UserData
	ctx      *Ctx
}

var availabilityJobs = make(chan magnetAvailabilityJob, 64)
var availabilityLock sync.Mutex
var pendingAvailability = make(map[string]bool)
var availabilityCache = cache.NewLRUCache[magnetAvailabilitySnapshot](&cache.CacheConfig{
	Name:     "stremio:torz:availability",
	Lifetime: 5 * time.Minute,
	MaxSize:  512,
})

func availabilityScopeKey(userScope, sid string) string {
	return util.MD5Hash(userScope + "|" + sid)
}

func init() {
	for i := 0; i < 3; i++ {
		go func() {
			for job := range availabilityJobs {
				func() {
					defer func() {
						availabilityLock.Lock()
						delete(pendingAvailability, job.key)
						availabilityLock.Unlock()
					}()
					result := job.userData.CheckMagnet(&store.CheckMagnetParams{
						Magnets:  job.hashes,
						ClientIP: job.ip,
						SId:      job.sid,
					}, job.ctx.Log)
					if result.HasErr && len(result.ByHash) == 0 {
						job.ctx.Log.Warn("background Torz magnet availability failed", "sid", job.sid)
						return
					}
					_ = availabilityCache.Add(job.key, magnetAvailabilitySnapshot{
						ByHash:       result.ByHash,
						FailedStores: result.HasErrByStoreCode,
						Count:        len(job.hashes),
					})
				}()
			}
		}()
	}
}

// getCachedAndQueueAvailability returns only *confirmed* cached hashes. When
// this user/media scope is cold, availability is unknown until background
// refresh completes; caller may still show uncached torrents if configured.
func getCachedAndQueueAvailability(ud *UserData, ctx *Ctx, userScope, sid string, hashes []string) (map[string]string, map[string]struct{}) {
	if len(hashes) == 0 {
		return nil, nil
	}
	key := availabilityScopeKey(userScope, sid)
	var snapshot magnetAvailabilitySnapshot
	found := availabilityCache.Get(key, &snapshot)
	if found && snapshot.Count == len(hashes) {
		return snapshot.ByHash, snapshot.FailedStores
	}
	availabilityLock.Lock()
	if !pendingAvailability[key] {
		pendingAvailability[key] = true
		// Keep the original slice independent from request-local append operations.
		copyHashes := append([]string(nil), hashes...)
		select {
		case availabilityJobs <- magnetAvailabilityJob{
			key: key, sid: sid, ip: ctx.ClientIP, hashes: copyHashes, userData: ud, ctx: ctx,
		}:
		default:
			delete(pendingAvailability, key)
			ctx.Log.Warn("background Torz availability queue is full", "sid", sid)
		}
	}
	availabilityLock.Unlock()
	if found {
		return snapshot.ByHash, snapshot.FailedStores
	}
	return nil, nil
}
