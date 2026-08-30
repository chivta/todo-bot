package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Counters are process-wide, following the same "initialise once, call via
// package functions" rule as the logger.
var (
	requestsTotal    atomic.Int64
	requestsRejected atomic.Int64
	jobsTotal        atomic.Int64
	jobsFailed       atomic.Int64
	listsCreated     atomic.Int64
	listsUpdated     atomic.Int64
	transcriptions   atomic.Int64
	queueDepth       atomic.Int64
	errorsTotal      atomic.Int64
)

// IncRequest records a request accepted from a user.
func IncRequest() { requestsTotal.Add(1) }

// IncRejected records a request turned away by rate limiting or a full queue.
func IncRejected() { requestsRejected.Add(1) }

// IncJob records a job that finished and was delivered.
func IncJob() { jobsTotal.Add(1) }

// IncJobFailed records a job that ended without a delivery.
func IncJobFailed() { jobsFailed.Add(1) }

// IncListCreated records a list built from scratch.
func IncListCreated() { listsCreated.Add(1) }

// IncListUpdated records a list changed by a reply.
func IncListUpdated() { listsUpdated.Add(1) }

// IncTranscription records a voice message sent for transcription.
func IncTranscription() { transcriptions.Add(1) }

// SetQueueDepth publishes how many jobs are waiting for a worker.
func SetQueueDepth(n int) { queueDepth.Store(int64(n)) }

// IncErrors records an error-level event. Called by the logging hook, not
// directly.
func IncErrors() { errorsTotal.Add(1) }

// Handler renders the counters in Prometheus text exposition format. Hand-rolled
// because a handful of atomics does not justify the client library's weight.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain; version=0.0.4")

		fmt.Fprintf(w, "# TYPE todobot_requests_total counter\ntodobot_requests_total %d\n", requestsTotal.Load())
		fmt.Fprintf(w, "# TYPE todobot_requests_rejected_total counter\ntodobot_requests_rejected_total %d\n", requestsRejected.Load())
		fmt.Fprintf(w, "# TYPE todobot_jobs_total counter\ntodobot_jobs_total %d\n", jobsTotal.Load())
		fmt.Fprintf(w, "# TYPE todobot_jobs_failed_total counter\ntodobot_jobs_failed_total %d\n", jobsFailed.Load())
		fmt.Fprintf(w, "# TYPE todobot_lists_created_total counter\ntodobot_lists_created_total %d\n", listsCreated.Load())
		fmt.Fprintf(w, "# TYPE todobot_lists_updated_total counter\ntodobot_lists_updated_total %d\n", listsUpdated.Load())
		fmt.Fprintf(w, "# TYPE todobot_transcriptions_total counter\ntodobot_transcriptions_total %d\n", transcriptions.Load())
		fmt.Fprintf(w, "# TYPE todobot_queue_depth gauge\ntodobot_queue_depth %d\n", queueDepth.Load())
		fmt.Fprintf(w, "# TYPE todobot_errors_total counter\ntodobot_errors_total %d\n", errorsTotal.Load())
	})
}
