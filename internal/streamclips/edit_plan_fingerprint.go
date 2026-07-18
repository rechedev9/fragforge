package streamclips

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// EditPlanFingerprint identifies one normalized edit-plan revision. Unlike the
// killfeed-analysis fingerprint, this includes every render-affecting field,
// reviewed subtitle/killfeed contents. UpdatedAt is deliberately excluded: it
// is not render input, and legacy plans may acquire it during normalization.
func EditPlanFingerprint(plan EditPlan) (string, error) {
	normalized := NormalizeEditPlan(plan)
	normalized.UpdatedAt = time.Time{}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode normalized stream edit plan: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
