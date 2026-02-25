package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const deleteConfirmationTTL = 15 * time.Minute
const softDeleteRecoveryWindow = 72 * time.Hour

type destructiveDeleteRequest struct {
	ConfirmationToken string `json:"confirmation_token"`
	HardDelete        bool   `json:"hard_delete"`
}

func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, out interface{}) error {
	if r == nil || out == nil {
		return nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		return err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}
