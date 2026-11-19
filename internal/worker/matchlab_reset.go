package worker

import (
	"errors"
	"sync"
	"time"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/job_log"
	"github.com/MunifTanjim/stremthru/internal/logger"
	"github.com/MunifTanjim/stremthru/internal/torrent_info"
	"github.com/google/uuid"
)

const matchLabResetJobName = "reset-matchlab"
const matchLabResetJobTTL = 7 * 24 * time.Hour

var ErrMatchLabResetInProgress = errors.New("a MatchLab reset is already running")
var matchLabResetLog = logger.Scoped("worker/matchlab-reset")
var matchLabResetExecute = torrent_info.ResetAllMatchLabMappingsWithStats

// MatchLabResetStatus is stored in the job log and can be read by any
// dashboard instance, including after a page reload.
type MatchLabResetStatus struct {
	ID             string    `json:"id,omitempty"`
	Status         string    `json:"status"`
	IncludeParsing bool      `json:"include_parsing"`
	Torrents       int64     `json:"torrents"`
	StreamFiles    int64     `json:"stream_files"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type matchLabResetData struct {
	IncludeParsing bool  `json:"include_parsing"`
	Torrents       int64 `json:"torrents"`
	StreamFiles    int64 `json:"stream_files"`
}

var matchLabResetState struct {
	sync.Mutex
	active bool
}

// GetMatchLabResetStatus returns the latest durable result. An interrupted
// process cannot leave a permanently "running" dashboard status: active
// jobs update their heartbeat every ten seconds.
func GetMatchLabResetStatus() (MatchLabResetStatus, error) {
	job, err := job_log.GetLastJobLog[matchLabResetData](matchLabResetJobName)
	if err != nil {
		return MatchLabResetStatus{}, err
	}
	if job == nil {
		return MatchLabResetStatus{Status: "idle"}, nil
	}
	status := MatchLabResetStatus{
		ID:        job.Id,
		Status:    job.Status,
		UpdatedAt: job.UpdatedAt,
		Error:     job.Error,
	}
	if job.Data != nil {
		status.IncludeParsing = job.Data.IncludeParsing
		status.Torrents = job.Data.Torrents
		status.StreamFiles = job.Data.StreamFiles
	}
	if status.Status == "started" && time.Since(job.UpdatedAt) > 5*time.Minute {
		status.Status = "interrupted"
		status.Error = "reset heartbeat expired; check the server logs before retrying"
	}
	return status, nil
}

// StartMatchLabReset schedules a long database transaction without tying it to
// an HTTP request or proxy timeout. The advisory lock serializes resets across
// StremThru replicas; the in-process guard also covers SQLite.
func StartMatchLabReset(includeParsing bool) (MatchLabResetStatus, error) {
	matchLabResetState.Lock()
	defer matchLabResetState.Unlock()

	if matchLabResetState.active {
		return MatchLabResetStatus{}, ErrMatchLabResetInProgress
	}

	lock := db.NewAdvisoryLock("maintenance", "reset-matchlab")
	if lock == nil {
		return MatchLabResetStatus{}, errors.New("failed to acquire MatchLab reset lock")
	}
	if !lock.TryAcquire() {
		_ = lock.ReleaseAll()
		if err := lock.Err(); err != nil {
			return MatchLabResetStatus{}, err
		}
		return MatchLabResetStatus{}, ErrMatchLabResetInProgress
	}

	id := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") + "-" + uuid.NewString()
	data := matchLabResetData{IncludeParsing: includeParsing}
	if err := job_log.SaveJobLog(matchLabResetJobName, id, "started", &data, "", matchLabResetJobTTL); err != nil {
		_ = lock.ReleaseAll()
		return MatchLabResetStatus{}, err
	}

	matchLabResetState.active = true
	go runMatchLabReset(id, data, lock)
	return MatchLabResetStatus{
		ID:             id,
		Status:         "started",
		IncludeParsing: includeParsing,
		UpdatedAt:      time.Now().UTC(),
	}, nil
}

func runMatchLabReset(id string, data matchLabResetData, lock db.AdvisoryLock) {
	defer func() {
		if !lock.ReleaseAll() {
			matchLabResetLog.Error("failed to release reset lock", "job", id, "error", lock.Err())
		}
		matchLabResetState.Lock()
		matchLabResetState.active = false
		matchLabResetState.Unlock()
	}()

	// Keep the persisted job live even if resetting millions of rows takes
	// longer than the reverse proxy or worker heartbeat interval.
	stop := make(chan struct{})
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := job_log.SaveJobLog(matchLabResetJobName, id, "started", &data, "", matchLabResetJobTTL); err != nil {
					matchLabResetLog.Error("reset heartbeat failed", "job", id, "error", err)
				}
			case <-stop:
				return
			}
		}
	}()

	stats, resetErr := matchLabResetExecute(data.IncludeParsing)
	close(stop)
	heartbeat.Wait()

	data.Torrents = stats.Torrents
	data.StreamFiles = stats.StreamFiles
	status := "done"
	errorMessage := ""
	if resetErr != nil {
		status = "failed"
		errorMessage = resetErr.Error()
	}
	if err := job_log.SaveJobLog(matchLabResetJobName, id, status, &data, errorMessage, matchLabResetJobTTL); err != nil {
		matchLabResetLog.Error("failed to persist reset result", "job", id, "error", err)
	}
	if resetErr != nil {
		matchLabResetLog.Error("MatchLab reset failed", "job", id, "error", resetErr)
	} else {
		matchLabResetLog.Info("MatchLab reset completed", "job", id, "torrents", stats.Torrents, "stream_files", stats.StreamFiles)
	}
}

