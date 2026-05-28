package workers

import (
	"log"
	"strings"

	"github.com/google/uuid"

	"github.com/reche/zackvideo/internal/job"
)

func logWorkerTransition(id uuid.UUID, taskType string, status job.Status) {
	log.Printf("worker job=%s task=%s transition=%s", id, taskType, status)
}

func logWorkerArtifacts(id uuid.UUID, taskType string, keys []string) {
	if len(keys) == 0 {
		return
	}
	log.Printf("worker job=%s task=%s artifact_keys=%s", id, taskType, strings.Join(keys, ","))
}

func logWorkerError(id uuid.UUID, op string, err error) {
	log.Printf("worker job=%s op=%s error=%v", id, op, err)
}

func logWorkerSkip(id uuid.UUID, taskType string, keys []string) {
	if len(keys) == 0 {
		log.Printf("worker job=%s task=%s skip=artifacts_ready", id, taskType)
		return
	}
	log.Printf("worker job=%s task=%s skip=artifacts_ready artifact_keys=%s", id, taskType, strings.Join(keys, ","))
}
