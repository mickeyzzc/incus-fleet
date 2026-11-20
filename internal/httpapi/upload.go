package httpapi

import (
	"net/http"
	"time"
)

// uploadDeadline is the budget for large multipart uploads (images and ISOs).
// The server's global ReadTimeout is tuned for regular API calls; a multi-
// gigabyte upload takes far longer, so the upload routes lift the deadline for
// the duration of the request.
const uploadDeadline = 2 * time.Hour

// extendUploadDeadline raises the read and write deadlines for one upload.
// http.ResponseController walks the handler chain, so the logging wrapper must
// forward Unwrap for this to reach the connection.
func extendUploadDeadline(w http.ResponseWriter) {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(uploadDeadline)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
}
