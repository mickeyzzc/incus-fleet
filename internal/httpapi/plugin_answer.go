package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

const maxWiredAnswerBytes = 1 << 20

// applyWiredAnswerFile consumes bootstrap.answer_file_url: when hive took over
// that wiring key, unattended installs fetch their answer file from it. An
// explicit answer file or stored answer profile still wins.
func (a *API) applyWiredAnswerFile(ctx context.Context, req *instances.UnattendedRequest) error {
	if a.registry == nil || strings.TrimSpace(req.AnswerFile) != "" || answerSpecProvided(req.Spec) {
		return nil
	}
	target, ok := a.registry.WiringValue("bootstrap.answer_file_url")
	if !ok || strings.TrimSpace(target) == "" {
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("%w: invalid wired answer file URL: %v", instances.ErrInvalidRequest, err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: fetch wired answer file: %v", instances.ErrInvalidRequest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: wired answer file returned %d", instances.ErrInvalidRequest, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWiredAnswerBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read wired answer file: %v", instances.ErrInvalidRequest, err)
	}
	if len(data) > maxWiredAnswerBytes {
		return fmt.Errorf("%w: wired answer file is larger than %d bytes", instances.ErrInvalidRequest, maxWiredAnswerBytes)
	}
	content := strings.TrimSpace(string(data))
	if content == "" || strings.ContainsRune(content, '\x00') {
		return fmt.Errorf("%w: wired answer file must be non-empty text", instances.ErrInvalidRequest)
	}
	req.AnswerFile = content

	return nil
}
